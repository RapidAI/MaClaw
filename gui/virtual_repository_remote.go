package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/sftp"
	"github.com/zalando/go-keyring"
	"golang.org/x/crypto/ssh"
)

const virtualRepositorySSHKeyringService = "MaClaw Virtual Repository SSH"

var (
	errRemoteVirtualRepositoryRootSymlink      = errors.New("remote root path must not be a symbolic link")
	errRemoteVirtualRepositoryRootNotDirectory = errors.New("remote root path exists but is not a directory")
)

type remoteVirtualRepositorySaveInput struct {
	Repository   VirtualRepository `json:"repository"`
	Password     string            `json:"password,omitempty"`
	TrustHostKey bool              `json:"trust_host_key,omitempty"`
}

type remoteVirtualRepositoryConnectionInput struct {
	RepositoryID string                   `json:"repository_id,omitempty"`
	Remote       *VirtualRepositoryRemote `json:"remote"`
	RootPath     string                   `json:"root_path"`
	Password     string                   `json:"password,omitempty"`
	TrustHostKey bool                     `json:"trust_host_key,omitempty"`
}

type RemoteVirtualRepositoryConnectionStatus struct {
	Connected          bool   `json:"connected"`
	HostKeyTrusted     bool   `json:"host_key_trusted"`
	HostKeyAlgorithm   string `json:"host_key_algorithm,omitempty"`
	HostKeyFingerprint string `json:"host_key_fingerprint,omitempty"`
	RootExists         bool   `json:"root_exists"`
	GitVersion         string `json:"git_version,omitempty"`
	SVNVersion         string `json:"svn_version,omitempty"`
	ErrorCode          string `json:"error_code,omitempty"`
	Error              string `json:"error,omitempty"`
}

type virtualRepositoryKnownHostFile struct {
	Version int               `json:"version"`
	Hosts   map[string]string `json:"hosts"`
}

type remoteHostKeyUntrustedError struct {
	Algorithm   string
	Fingerprint string
}

func (e *remoteHostKeyUntrustedError) Error() string {
	return fmt.Sprintf("SSH host key is not trusted (%s %s)", e.Algorithm, e.Fingerprint)
}

func remoteVirtualRepositoryHostID(remote *VirtualRepositoryRemote) string {
	port := remote.Port
	if port == 0 {
		port = 22
	}
	return strings.ToLower(strings.TrimSpace(remote.Host)) + ":" + strconv.Itoa(port)
}

func sshHostKeyFingerprint(key ssh.PublicKey) string {
	sum := sha256.Sum256(key.Marshal())
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

func (a *App) loadVirtualRepositoryKnownHosts() (virtualRepositoryKnownHostFile, error) {
	file := virtualRepositoryKnownHostFile{Version: 1, Hosts: map[string]string{}}
	if err := readJSONFile(a.virtualRepositoryStatePath("virtual-repository-known-hosts.json"), &file); err != nil {
		return file, err
	}
	if file.Version != 1 {
		return file, fmt.Errorf("unsupported virtual repository known-hosts file version %d", file.Version)
	}
	if file.Hosts == nil {
		file.Hosts = map[string]string{}
	}
	for hostID, fingerprint := range file.Hosts {
		if strings.TrimSpace(hostID) == "" || strings.TrimSpace(fingerprint) == "" || len(hostID) > virtualRepositoryFieldMaxLength || len(fingerprint) > virtualRepositoryFieldMaxLength || containsControlCharacter(hostID) || containsControlCharacter(fingerprint) {
			return file, errors.New("virtual repository known-hosts file contains an invalid entry")
		}
	}
	return file, nil
}

func (a *App) remoteVirtualRepositoryPassword(repositoryID, supplied string) (string, error) {
	if strings.TrimSpace(supplied) != "" {
		return supplied, nil
	}
	if strings.TrimSpace(repositoryID) == "" {
		return "", errors.New("SSH password is required")
	}
	password, err := keyring.Get(virtualRepositorySSHKeyringService, repositoryID)
	if err != nil {
		return "", fmt.Errorf("read SSH password from system keyring: %w", err)
	}
	return password, nil
}

func (a *App) dialRemoteVirtualRepository(ctx context.Context, repositoryID string, remote *VirtualRepositoryRemote, suppliedPassword string, trustHostKey bool) (*ssh.Client, string, string, error) {
	if remote == nil {
		return nil, "", "", errors.New("remote SSH configuration is required")
	}
	password, err := a.remoteVirtualRepositoryPassword(repositoryID, suppliedPassword)
	if err != nil {
		return nil, "", "", err
	}
	virtualRepositoryKnownHostsMu.Lock()
	knownHosts, err := a.loadVirtualRepositoryKnownHosts()
	virtualRepositoryKnownHostsMu.Unlock()
	if err != nil {
		return nil, "", "", err
	}
	hostID := remoteVirtualRepositoryHostID(remote)
	expected := knownHosts.Hosts[hostID]
	var observedFingerprint, observedAlgorithm string
	callback := func(_ string, _ net.Addr, key ssh.PublicKey) error {
		observedFingerprint, observedAlgorithm = sshHostKeyFingerprint(key), key.Type()
		if expected != "" && expected != observedFingerprint {
			return fmt.Errorf("SSH host key changed for %s: expected %s, received %s", hostID, expected, observedFingerprint)
		}
		if expected == "" && !trustHostKey {
			return &remoteHostKeyUntrustedError{Algorithm: observedAlgorithm, Fingerprint: observedFingerprint}
		}
		return nil
	}
	port := remote.Port
	if port == 0 {
		port = 22
	}
	config := &ssh.ClientConfig{
		User: strings.TrimSpace(remote.User),
		Auth: []ssh.AuthMethod{ssh.Password(password), ssh.KeyboardInteractive(func(_ string, _ string, questions []string, _ []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i := range answers {
				answers[i] = password
			}
			return answers, nil
		})},
		HostKeyCallback: callback,
		Timeout:         12 * time.Second,
	}
	addr := net.JoinHostPort(strings.TrimSpace(remote.Host), strconv.Itoa(port))
	dialer := net.Dialer{Timeout: 12 * time.Second}
	tcp, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, observedAlgorithm, observedFingerprint, fmt.Errorf("connect SSH %s: %w", addr, err)
	}
	connection, channels, requests, err := ssh.NewClientConn(tcp, addr, config)
	if err != nil {
		_ = tcp.Close()
		return nil, observedAlgorithm, observedFingerprint, fmt.Errorf("SSH handshake %s: %w", addr, err)
	}
	client := ssh.NewClient(connection, channels, requests)
	if expected == "" && trustHostKey {
		virtualRepositoryKnownHostsMu.Lock()
		latest, loadErr := a.loadVirtualRepositoryKnownHosts()
		if loadErr == nil {
			if pinned := latest.Hosts[hostID]; pinned != "" && pinned != observedFingerprint {
				loadErr = fmt.Errorf("SSH host key changed for %s while saving trust", hostID)
			} else {
				latest.Hosts[hostID] = observedFingerprint
				loadErr = writeJSONFile(a.virtualRepositoryStatePath("virtual-repository-known-hosts.json"), latest)
			}
		}
		virtualRepositoryKnownHostsMu.Unlock()
		if loadErr != nil {
			_ = client.Close()
			return nil, observedAlgorithm, observedFingerprint, loadErr
		}
	}
	return client, observedAlgorithm, observedFingerprint, nil
}

func runRemoteRepositoryCommand(ctx context.Context, client *ssh.Client, command string) (string, error) {
	return runRemoteRepositoryCommandInput(ctx, client, command, "")
}

func runRemoteRepositoryCommandInput(ctx context.Context, client *ssh.Client, command, stdin string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	stdout := &limitedVCSBuffer{limit: 512 * 1024}
	stderr := &limitedVCSBuffer{limit: 512 * 1024}
	session.Stdout, session.Stderr = stdout, stderr
	if stdin != "" {
		session.Stdin = strings.NewReader(stdin)
	}
	done := make(chan error, 1)
	go func() { done <- session.Run(command) }()
	select {
	case <-ctx.Done():
		_ = session.Close()
		return "", ctx.Err()
	case err := <-done:
		if err != nil {
			message := redactVCSOutput(stderr.String())
			if message == "" {
				message = err.Error()
			}
			return redactVCSOutput(stdout.String()), errors.New(message)
		}
		return redactVCSOutput(stdout.String()), nil
	}
}

type remoteGitAskPass struct {
	scriptPath, usernamePath, secretPath string
	cleanup                              func()
}

func createRemoteGitAskPass(client *ssh.Client, username, secret string) (remoteGitAskPass, error) {
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return remoteGitAskPass{}, fmt.Errorf("start SFTP for Git credential: %w", err)
	}
	prefix := "/tmp/maclaw-vrepo-auth-" + uuid.NewString()
	result := remoteGitAskPass{scriptPath: prefix + ".sh", usernamePath: prefix + ".user", secretPath: prefix + ".secret"}
	result.cleanup = func() {
		_ = sftpClient.Remove(result.scriptPath)
		_ = sftpClient.Remove(result.usernamePath)
		_ = sftpClient.Remove(result.secretPath)
		_ = sftpClient.Close()
	}
	write := func(name string, data []byte, mode os.FileMode) error {
		file, openErr := sftpClient.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
		if openErr != nil {
			return openErr
		}
		_, writeErr := file.Write(data)
		if writeErr == nil {
			writeErr = file.Chmod(mode)
		}
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		return closeErr
	}
	script := "#!/bin/sh\ncase \"$1\" in\n  *sername*) cat -- \"$MACLAW_VREPO_GIT_USER_FILE\" ;;\n  *) cat -- \"$MACLAW_VREPO_GIT_SECRET_FILE\" ;;\nesac\n"
	if err := write(result.scriptPath, []byte(script), 0o700); err != nil {
		result.cleanup()
		return remoteGitAskPass{}, err
	}
	if err := write(result.usernamePath, []byte(username), 0o600); err != nil {
		result.cleanup()
		return remoteGitAskPass{}, err
	}
	if err := write(result.secretPath, []byte(secret), 0o600); err != nil {
		result.cleanup()
		return remoteGitAskPass{}, err
	}
	return result, nil
}

func (a *App) TestRemoteVirtualRepositoryConnection(inputJSON string) (string, error) {
	var input remoteVirtualRepositoryConnectionInput
	if err := unmarshalVirtualRepositoryInput(inputJSON, "remote virtual repository connection", &input); err != nil {
		return "", err
	}
	probe := VirtualRepository{Name: "Connection test", RootPath: input.RootPath, Remote: cloneVirtualRepositoryRemote(input.Remote), Nodes: []VirtualRepositoryNode{}}
	if err := validateVirtualRepository(&probe); err != nil {
		return "", err
	}
	input.Remote, input.RootPath = probe.Remote, probe.RootPath
	if len(input.RepositoryID) > virtualRepositoryNameMaxLength || containsControlCharacter(input.RepositoryID) || len(input.Password) > virtualRepositoryFieldMaxLength || strings.ContainsAny(input.Password, "\r\n\x00") {
		return "", errors.New("remote virtual repository connection contains an invalid credential field")
	}
	status := RemoteVirtualRepositoryConnectionStatus{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client, algorithm, fingerprint, err := a.dialRemoteVirtualRepository(ctx, input.RepositoryID, input.Remote, input.Password, input.TrustHostKey)
	status.HostKeyAlgorithm, status.HostKeyFingerprint = algorithm, fingerprint
	if err != nil {
		var untrusted *remoteHostKeyUntrustedError
		if errors.As(err, &untrusted) {
			status.ErrorCode, status.Error = "host_key_untrusted", err.Error()
			status.HostKeyAlgorithm, status.HostKeyFingerprint = untrusted.Algorithm, untrusted.Fingerprint
		} else {
			status.ErrorCode, status.Error = "connection_failed", err.Error()
		}
		data, marshalErr := json.Marshal(status)
		return string(data), marshalErr
	}
	defer client.Close()
	status.Connected, status.HostKeyTrusted = true, true
	status.RootExists, err = remoteVirtualRepositoryRootDirectoryExists(client, input.RootPath)
	status.GitVersion, _ = runRemoteRepositoryCommand(ctx, client, "git --version")
	status.SVNVersion, _ = runRemoteRepositoryCommand(ctx, client, "svn --version --quiet")
	if errors.Is(err, errRemoteVirtualRepositoryRootSymlink) {
		status.ErrorCode, status.Error = "root_symlink", "SSH connected, but the remote root path is a symbolic link"
	} else if errors.Is(err, errRemoteVirtualRepositoryRootNotDirectory) {
		status.ErrorCode, status.Error = "root_not_directory", "SSH connected, but the remote root path is not a directory"
	} else if err != nil {
		status.ErrorCode, status.Error = "root_check_failed", "SSH connected, but the remote root directory could not be checked"
	} else if !status.RootExists {
		status.ErrorCode, status.Error = "root_not_found", "SSH connected, but remote root directory does not exist"
	}
	data, marshalErr := json.Marshal(status)
	return string(data), marshalErr
}

// CreateRemoteVirtualRepositoryRoot creates a missing root only after an
// explicit frontend confirmation. It deliberately accepts the same connection
// envelope as the probe so passwords remain transient and host-key pinning is
// enforced by the normal SSH dial path.
func (a *App) CreateRemoteVirtualRepositoryRoot(inputJSON string) (resultErr error) {
	started := time.Now()
	var input remoteVirtualRepositoryConnectionInput
	defer func() {
		status := "success"
		if resultErr != nil {
			status = "failed"
		}
		log.Printf("[vrepo] create_remote_root repo=%q status=%s duration_ms=%d error=%q", strings.TrimSpace(input.RepositoryID), status, time.Since(started).Milliseconds(), virtualRepositoryLogError(resultErr))
	}()
	if err := unmarshalVirtualRepositoryInput(inputJSON, "remote virtual repository connection", &input); err != nil {
		return err
	}
	probe := VirtualRepository{Name: "Create remote root", RootPath: input.RootPath, Remote: cloneVirtualRepositoryRemote(input.Remote), Nodes: []VirtualRepositoryNode{}}
	if err := validateVirtualRepository(&probe); err != nil {
		return err
	}
	input.Remote, input.RootPath = probe.Remote, probe.RootPath
	if len(input.RepositoryID) > virtualRepositoryNameMaxLength || containsControlCharacter(input.RepositoryID) || len(input.Password) > virtualRepositoryFieldMaxLength || strings.ContainsAny(input.Password, "\r\n\x00") {
		return errors.New("remote virtual repository connection contains an invalid credential field")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, _, _, err := a.dialRemoteVirtualRepository(ctx, input.RepositoryID, input.Remote, input.Password, input.TrustHostKey)
	if err != nil {
		return err
	}
	defer client.Close()
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return fmt.Errorf("start SFTP: %w", err)
	}
	defer sftpClient.Close()
	return createRemoteVirtualRepositoryRootDirectory(sftpClient, input.RootPath)
}

// createRemoteVirtualRepositoryRootDirectory creates one path component at a
// time so an existing symbolic-link ancestor cannot redirect the confirmed
// path to an unexpected location.
func createRemoteVirtualRepositoryRootDirectory(sftpClient *sftp.Client, root string) error {
	current := "/"
	for _, component := range strings.Split(strings.TrimPrefix(path.Clean(root), "/"), "/") {
		if component == "" || component == "." {
			continue
		}
		current = path.Join(current, component)
		info, err := sftpClient.Lstat(current)
		if os.IsNotExist(err) {
			if err := sftpClient.Mkdir(current); err != nil {
				return fmt.Errorf("create remote root directory component %q: %w", current, err)
			}
			info, err = sftpClient.Lstat(current)
		}
		if err != nil {
			return fmt.Errorf("check remote root directory component %q: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("remote root path component %q is a symbolic link; create the directory manually after verifying the target", current)
		}
		if !info.IsDir() {
			return fmt.Errorf("remote root path component %q exists but is not a directory", current)
		}
	}
	return nil
}

func (a *App) writeRemoteVirtualRepository(client *ssh.Client, repo *VirtualRepository) error {
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return fmt.Errorf("start SFTP: %w", err)
	}
	defer sftpClient.Close()
	if err := validateRemoteVirtualRepositoryRootDirectory(sftpClient, repo.RootPath); err != nil {
		return err
	}
	dir, err := ensureRemoteVirtualRepositoryMetadataDirectory(sftpClient, repo.RootPath)
	if err != nil {
		return err
	}
	disk := *repo
	disk.RootPath = ""
	disk.Remote = nil
	data, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return err
	}
	if len(data)+1 > virtualRepositoryManifestMaxBytes {
		return fmt.Errorf("remote .vrepo manifest exceeds %d bytes", virtualRepositoryManifestMaxBytes)
	}
	data = append(data, '\n')
	temporary := path.Join(dir, ".manifest-"+uuid.NewString()+".tmp")
	file, err := sftpClient.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return err
	}
	if _, err = io.Copy(file, bytes.NewReader(data)); err == nil {
		err = file.Chmod(0o600)
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = sftpClient.Remove(temporary)
		return err
	}
	destination := path.Join(dir, virtualRepositoryManifestName)
	err = sftpClient.PosixRename(temporary, destination)
	if err != nil {
		_ = sftpClient.Remove(temporary)
		return fmt.Errorf("remote server does not support atomic POSIX rename for .vrepo manifest: %w", err)
	}
	return nil
}

func ensureRemoteVirtualRepositoryMetadataDirectory(sftpClient *sftp.Client, root string) (string, error) {
	dir := path.Join(root, virtualRepositoryDirName)
	info, err := sftpClient.Lstat(dir)
	if os.IsNotExist(err) {
		if mkdirErr := sftpClient.Mkdir(dir); mkdirErr != nil && !os.IsExist(mkdirErr) {
			return "", fmt.Errorf("create remote .vrepo directory: %w", mkdirErr)
		}
		info, err = sftpClient.Lstat(dir)
	}
	if err != nil {
		return "", fmt.Errorf("check remote .vrepo directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("remote .vrepo path must not be a symbolic link")
	}
	if !info.IsDir() {
		return "", errors.New("remote .vrepo path exists but is not a directory")
	}
	return dir, nil
}

func remoteVirtualRepositoryManifestExists(client *ssh.Client, root string) (bool, error) {
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return false, fmt.Errorf("start SFTP: %w", err)
	}
	defer sftpClient.Close()
	_, err = sftpClient.Stat(path.Join(root, virtualRepositoryDirName, virtualRepositoryManifestName))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("check remote .vrepo manifest: %w", err)
}

func (a *App) readRemoteVirtualRepository(item virtualRepositoryIndexEntry, suppliedPassword string, trust bool) (*VirtualRepository, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client, _, _, err := a.dialRemoteVirtualRepository(ctx, item.ID, item.Remote, suppliedPassword, trust)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	return a.readRemoteVirtualRepositoryWithClient(client, item)
}

func (a *App) SaveRemoteVirtualRepository(inputJSON string) (string, error) {
	started := time.Now()
	// Serialize remote compare-and-swap saves without holding the local state
	// mutex across SSH/SFTP I/O. Local repository and credential work stays responsive.
	virtualRepositoryRemoteSaveMu.Lock()
	defer virtualRepositoryRemoteSaveMu.Unlock()
	var input remoteVirtualRepositorySaveInput
	if err := unmarshalVirtualRepositoryInput(inputJSON, "remote virtual repository", &input); err != nil {
		return "", err
	}
	if len(input.Password) > virtualRepositoryFieldMaxLength || strings.ContainsAny(input.Password, "\r\n\x00") {
		return "", errors.New("remote virtual repository password is invalid")
	}
	repo := input.Repository
	if repo.Remote == nil {
		return "", errors.New("remote SSH configuration is required")
	}
	isNew := strings.TrimSpace(repo.ID) == ""
	var previousLocation *virtualRepositoryIndexEntry
	if isNew {
		// Fail on a corrupt/incompatible local index before connecting, trusting a
		// host key, storing a password, or creating a remote manifest.
		if _, err := a.loadVirtualRepositoryIndexItems(); err != nil {
			return "", err
		}
		repo.ID = "vrepo_" + uuid.NewString()
	} else {
		if repo.UpdatedAt.IsZero() {
			return "", errors.New("existing remote virtual repository revision is required; reopen it before saving")
		}
		found := false
		indexItems, err := a.loadVirtualRepositoryIndexItems()
		if err != nil {
			return "", err
		}
		for _, item := range indexItems {
			if item.ID != repo.ID || item.Remote == nil {
				continue
			}
			found = true
			previousLocation = &virtualRepositoryIndexEntry{ID: item.ID, Name: item.Name, RootPath: item.RootPath, Remote: cloneVirtualRepositoryRemote(item.Remote), LastOpened: item.LastOpened}
			// A password supplied for a different SSH endpoint must never be sent
			// to the old host. On the same endpoint it may be the replacement for
			// an expired password and is required to authorize that update.
			readPassword, readTrust := input.Password, input.TrustHostKey
			if !sameRemoteVirtualRepositoryEndpoint(item.Remote, repo.Remote) {
				readPassword, readTrust = "", false
			}
			current, readErr := a.readRemoteVirtualRepository(item, readPassword, readTrust)
			if readErr != nil {
				return "", readErr
			}
			if !current.UpdatedAt.Equal(repo.UpdatedAt) {
				return "", errors.New("remote virtual repository was modified by another window; reopen it before saving")
			}
			break
		}
		if !found {
			return "", errors.New("remote virtual repository was not found in the local index; reopen it before saving")
		}
	}
	now := time.Now().UTC()
	if repo.CreatedAt.IsZero() {
		repo.CreatedAt = now
	}
	repo.UpdatedAt = now
	if err := validateVirtualRepository(&repo); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, _, _, err := a.dialRemoteVirtualRepository(ctx, repo.ID, repo.Remote, input.Password, input.TrustHostKey)
	if err != nil {
		return "", err
	}
	defer client.Close()
	rootExists, rootCheckErr := remoteVirtualRepositoryRootDirectoryExists(client, repo.RootPath)
	if rootCheckErr != nil {
		return "", rootCheckErr
	}
	if !rootExists {
		return "", errors.New("remote root directory does not exist; test the connection and explicitly create it first")
	}
	destinationChanged := previousLocation != nil && (!sameRemoteVirtualRepositoryEndpoint(previousLocation.Remote, repo.Remote) || previousLocation.RootPath != repo.RootPath)
	if isNew || destinationChanged {
		exists, checkErr := remoteVirtualRepositoryManifestExists(client, repo.RootPath)
		if checkErr != nil {
			return "", checkErr
		}
		if exists {
			return "", errors.New("this remote root already contains a virtual repository; open the existing repository instead of creating a new one")
		}
	}
	var previousPassword string
	hadPreviousPassword := false
	if strings.TrimSpace(input.Password) != "" {
		if current, getErr := keyring.Get(virtualRepositorySSHKeyringService, repo.ID); getErr == nil {
			previousPassword, hadPreviousPassword = current, true
		}
		if err := keyring.Set(virtualRepositorySSHKeyringService, repo.ID, input.Password); err != nil {
			return "", fmt.Errorf("save SSH password in system keyring: %w", err)
		}
	}
	if err := a.writeRemoteVirtualRepository(client, &repo); err != nil {
		log.Printf("[vrepo] save_remote repo=%q nodes=%d status=manifest_failed duration_ms=%d error=%q", repo.ID, len(repo.Nodes), time.Since(started).Milliseconds(), virtualRepositoryLogError(err))
		if strings.TrimSpace(input.Password) != "" {
			if hadPreviousPassword {
				_ = keyring.Set(virtualRepositorySSHKeyringService, repo.ID, previousPassword)
			} else {
				_ = keyring.Delete(virtualRepositorySSHKeyringService, repo.ID)
			}
		}
		return "", err
	}
	if err := a.updateVirtualRepositoryIndex(&repo); err != nil {
		log.Printf("[vrepo] save_remote repo=%q nodes=%d status=index_failed duration_ms=%d error=%q", repo.ID, len(repo.Nodes), time.Since(started).Milliseconds(), virtualRepositoryLogError(err))
		if strings.TrimSpace(input.Password) != "" {
			if hadPreviousPassword {
				_ = keyring.Set(virtualRepositorySSHKeyringService, repo.ID, previousPassword)
			} else {
				_ = keyring.Delete(virtualRepositorySSHKeyringService, repo.ID)
			}
		}
		return "", fmt.Errorf("remote manifest was saved, but the local recent-repository index could not be updated: %w", err)
	}
	log.Printf("[vrepo] save_remote repo=%q nodes=%d status=success duration_ms=%d", repo.ID, len(repo.Nodes), time.Since(started).Milliseconds())
	data, err := json.Marshal(repo)
	return string(data), err
}

func sameRemoteVirtualRepositoryEndpoint(left, right *VirtualRepositoryRemote) bool {
	if left == nil || right == nil {
		return left == right
	}
	return remoteVirtualRepositoryHostID(left) == remoteVirtualRepositoryHostID(right) && strings.TrimSpace(left.User) == strings.TrimSpace(right.User)
}

func remoteVirtualRepositoryRootDirectoryExists(client *ssh.Client, root string) (bool, error) {
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return false, fmt.Errorf("start SFTP: %w", err)
	}
	defer sftpClient.Close()
	info, err := sftpClient.Lstat(root)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check remote root directory: %w", err)
	}
	if err := validateRemoteVirtualRepositoryRootDirectoryInfo(info); err != nil {
		return false, err
	}
	return true, nil
}

func validateRemoteVirtualRepositoryRootDirectory(sftpClient *sftp.Client, root string) error {
	info, err := sftpClient.Lstat(root)
	if os.IsNotExist(err) {
		return errors.New("remote root directory does not exist; test the connection and explicitly create it first")
	}
	if err != nil {
		return fmt.Errorf("check remote root directory: %w", err)
	}
	return validateRemoteVirtualRepositoryRootDirectoryInfo(info)
}

func validateRemoteVirtualRepositoryRootDirectoryInfo(info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return errRemoteVirtualRepositoryRootSymlink
	}
	if !info.IsDir() {
		return errRemoteVirtualRepositoryRootNotDirectory
	}
	return nil
}

func (a *App) OpenRemoteVirtualRepository(id string) (string, error) {
	indexItems, err := a.loadVirtualRepositoryIndexItems()
	if err != nil {
		return "", err
	}
	for _, item := range indexItems {
		if item.ID == strings.TrimSpace(id) && item.Remote != nil {
			repo, err := a.readRemoteVirtualRepository(item, "", false)
			if err != nil {
				return "", err
			}
			data, err := json.Marshal(repo)
			return string(data), err
		}
	}
	return "", errors.New("remote virtual repository was not found")
}

func (a *App) remoteVirtualRepositoryByID(id string) (*VirtualRepository, *ssh.Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return a.remoteVirtualRepositoryByIDContext(ctx, id)
}

func (a *App) remoteVirtualRepositoryByIDContext(ctx context.Context, id string) (*VirtualRepository, *ssh.Client, error) {
	indexItems, err := a.loadVirtualRepositoryIndexItems()
	if err != nil {
		return nil, nil, err
	}
	for _, item := range indexItems {
		if item.ID != strings.TrimSpace(id) || item.Remote == nil {
			continue
		}
		client, _, _, err := a.dialRemoteVirtualRepository(ctx, item.ID, item.Remote, "", false)
		if err != nil {
			return nil, nil, err
		}
		repo, err := a.readRemoteVirtualRepositoryWithClient(client, item)
		if err != nil {
			_ = client.Close()
			return nil, nil, err
		}
		return repo, client, nil
	}
	return nil, nil, errors.New("remote virtual repository was not found")
}

func (a *App) readRemoteVirtualRepositoryWithClient(client *ssh.Client, item virtualRepositoryIndexEntry) (*VirtualRepository, error) {
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return nil, err
	}
	defer sftpClient.Close()
	file, err := sftpClient.Open(path.Join(item.RootPath, virtualRepositoryDirName, virtualRepositoryManifestName))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, virtualRepositoryManifestMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > virtualRepositoryManifestMaxBytes {
		return nil, fmt.Errorf("remote .vrepo manifest exceeds %d bytes", virtualRepositoryManifestMaxBytes)
	}
	var repo VirtualRepository
	if err := json.Unmarshal(data, &repo); err != nil {
		return nil, fmt.Errorf("parse remote .vrepo manifest: %w", err)
	}
	if repo.ID != item.ID {
		return nil, errors.New("virtual repository index no longer matches its remote manifest")
	}
	repo.RootPath, repo.Remote = item.RootPath, item.Remote
	if err := validateVirtualRepository(&repo); err != nil {
		return nil, err
	}
	return &repo, nil
}

func remoteVirtualRepositoryNodePath(root, relative string) string {
	return path.Join(strings.TrimRight(root, "/"), path.Clean(relative))
}

func inspectRemoteVirtualRepositoryNode(ctx context.Context, client *ssh.Client, repo *VirtualRepository, node VirtualRepositoryNode) VirtualRepositoryNodeStatus {
	binding := node.Repository
	target := remoteVirtualRepositoryNodePath(repo.RootPath, binding.RelativePath)
	status := VirtualRepositoryNodeStatus{NodeID: node.ID, Kind: binding.Kind, Path: target}
	const missingMarker = "__MACLAW_VREPO_NOT_CHECKED_OUT__"
	realRootCmd := "root=$(realpath " + remoteShellQuote(repo.RootPath) + ") || exit 21; target=" + remoteShellQuote(target) + "; if [ -L \"$target\" ] && [ ! -e \"$target\" ]; then exit 24; fi; if [ ! -e \"$target\" ]; then ancestor=\"$target\"; while [ ! -e \"$ancestor\" ]; do next=$(dirname -- \"$ancestor\"); [ \"$next\" = \"$ancestor\" ] && exit 23; ancestor=\"$next\"; done; realancestor=$(realpath \"$ancestor\") || exit 22; case \"$realancestor\" in \"$root\"|\"$root\"/*) printf '%s' " + remoteShellQuote(missingMarker) + ";; *) exit 23;; esac; exit 0; fi; target=$(realpath \"$target\") || exit 22; case \"$target\" in \"$root\"/*) printf '%s' \"$target\";; *) exit 23;; esac"
	resolved, err := runRemoteRepositoryCommand(ctx, client, realRootCmd)
	if err != nil {
		status.ErrorCode, status.Error = "path_invalid", err.Error()
		return status
	}
	if strings.TrimSpace(resolved) == missingMarker {
		status.ErrorCode, status.Error = "not_checked_out", "repository has not been checked out"
		return status
	}
	status.Path, status.Exists = strings.TrimSpace(resolved), true
	if binding.Kind == "local" {
		status.IsRepository, status.Clean, status.Status = true, true, "local directory"
		return status
	}
	quoted := remoteShellQuote(status.Path)
	if binding.Kind == "git" {
		inside, runErr := runRemoteRepositoryCommand(ctx, client, "git -C "+quoted+" rev-parse --is-inside-work-tree")
		if runErr != nil || strings.TrimSpace(inside) != "true" {
			status.ErrorCode, status.Error = "not_working_copy", virtualRepositoryErrorText(runErr, "not a Git working tree")
			return status
		}
		status.IsRepository = true
		if binding.RefType == "tag" && binding.RefName != "" {
			status.Branch, err = runRemoteRepositoryCommand(ctx, client, "git -C "+quoted+" describe --tags --exact-match")
			if err != nil {
				status.ErrorCode, status.Error = "ref_mismatch", fmt.Sprintf("configured tag %q is not checked out", binding.RefName)
				return status
			}
		} else {
			status.Branch, err = runRemoteRepositoryCommand(ctx, client, "git -C "+quoted+" symbolic-ref --quiet --short HEAD")
			if err != nil {
				status.ErrorCode, status.Error = "command_failed", err.Error()
				return status
			}
		}
		status.RemoteURL, err = runRemoteRepositoryCommand(ctx, client, "git -C "+quoted+" remote get-url origin")
		if err != nil {
			status.ErrorCode, status.Error = "remote_unavailable", err.Error()
			return status
		}
		status.Status, err = runRemoteRepositoryCommand(ctx, client, "git -C "+quoted+" status --short")
	} else {
		_, runErr := runRemoteRepositoryCommand(ctx, client, "svn info --show-item wc-root "+quoted)
		if runErr != nil {
			status.ErrorCode, status.Error = "not_working_copy", runErr.Error()
			return status
		}
		status.IsRepository = true
		status.RemoteURL, _ = runRemoteRepositoryCommand(ctx, client, "svn info --show-item url "+quoted)
		status.Status, err = runRemoteRepositoryCommand(ctx, client, "svn status "+quoted)
	}
	if err != nil {
		status.ErrorCode, status.Error = "command_failed", err.Error()
		return status
	}
	status.Branch = strings.TrimSpace(status.Branch)
	status.RemoteURL = sanitizeRepositoryRemoteURL(status.RemoteURL)
	expectedRemote := binding.RemoteURL
	if binding.Kind == "svn" {
		expectedRemote = svnRepositoryURLForBinding(binding)
	}
	if expected := normalizeRepositoryRemoteURL(expectedRemote); expected != "" && normalizeRepositoryRemoteURL(status.RemoteURL) != expected {
		status.ErrorCode, status.Error = "remote_mismatch", fmt.Sprintf("configured repository URL does not match working copy remote (%s)", status.RemoteURL)
		return status
	}
	if binding.Kind == "git" && binding.RefName != "" && status.Branch != binding.RefName {
		status.ErrorCode, status.Error = "ref_mismatch", fmt.Sprintf("configured %s %q is not checked out (current: %s)", binding.RefType, binding.RefName, status.Branch)
		return status
	}
	status.Status = strings.TrimSpace(status.Status)
	status.Clean = status.Status == ""
	return status
}

func (a *App) InspectRemoteVirtualRepository(id string) (string, error) {
	started := time.Now()
	repo, client, err := a.remoteVirtualRepositoryByID(id)
	if err != nil {
		log.Printf("[vrepo] inspect_remote repo=%q status=connect_failed duration_ms=%d error=%q", strings.TrimSpace(id), time.Since(started).Milliseconds(), virtualRepositoryLogError(err))
		return "", err
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	statuses := []VirtualRepositoryNodeStatus{}
	for _, node := range repo.Nodes {
		if node.Repository != nil && node.Repository.Enabled {
			statuses = append(statuses, inspectRemoteVirtualRepositoryNode(ctx, client, repo, node))
		}
	}
	data, err := json.Marshal(statuses)
	errorsFound := 0
	for _, status := range statuses {
		if status.ErrorCode != "" {
			errorsFound++
		}
	}
	log.Printf("[vrepo] inspect_remote repo=%q checked=%d errors=%d duration_ms=%d", repo.ID, len(statuses), errorsFound, time.Since(started).Milliseconds())
	return string(data), err
}

func (a *App) CreateRemoteVirtualRepositoryDirectory(repositoryID, relative string) error {
	if err := validateRemoteVirtualRepositoryRelativePath(relative); err != nil {
		return err
	}
	repo, client, err := a.remoteVirtualRepositoryByID(repositoryID)
	if err != nil {
		return err
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err = runRemoteRepositoryCommand(ctx, client, "root=$(realpath "+remoteShellQuote(repo.RootPath)+") || exit 21; target="+remoteShellQuote(remoteVirtualRepositoryNodePath(repo.RootPath, relative))+"; ancestor=\"$target\"; while [ ! -e \"$ancestor\" ]; do next=$(dirname -- \"$ancestor\"); [ \"$next\" = \"$ancestor\" ] && exit 23; ancestor=\"$next\"; done; realancestor=$(realpath \"$ancestor\") || exit 22; case \"$realancestor\" in \"$root\"|\"$root\"/*) mkdir -p -- \"$target\";; *) exit 23;; esac; realtarget=$(realpath \"$target\") || exit 22; case \"$realtarget\" in \"$root\"/*) :;; *) exit 23;; esac")
	return err
}

func (a *App) CheckoutRemoteVirtualRepositoryNode(repositoryID, nodeID string) (resultErr error) {
	started := time.Now()
	kind, refType, refName := "", "", ""
	defer func() {
		status := "success"
		if resultErr != nil {
			status = "failed"
		}
		log.Printf("[vrepo] checkout_remote repo=%q node=%q kind=%q ref_type=%q ref=%q status=%s duration_ms=%d error=%q", repositoryID, nodeID, kind, refType, refName, status, time.Since(started).Milliseconds(), virtualRepositoryLogError(resultErr))
	}()
	repositoryID, nodeID = strings.TrimSpace(repositoryID), strings.TrimSpace(nodeID)
	if repositoryID == "" || nodeID == "" || len(repositoryID) > virtualRepositoryNameMaxLength || len(nodeID) > virtualRepositoryNameMaxLength || containsControlCharacter(repositoryID) || containsControlCharacter(nodeID) {
		return errors.New("virtual repository or node id is invalid")
	}
	repo, client, err := a.remoteVirtualRepositoryByID(repositoryID)
	if err != nil {
		return err
	}
	defer client.Close()
	var node *VirtualRepositoryNode
	for i := range repo.Nodes {
		if repo.Nodes[i].ID == nodeID {
			node = &repo.Nodes[i]
			break
		}
	}
	if node == nil || node.Repository == nil || node.Repository.Kind == "local" || !node.Repository.Enabled {
		return errors.New("version-controlled virtual repository node was not found")
	}
	kind, refType, refName = node.Repository.Kind, node.Repository.RefType, node.Repository.RefName
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	target := remoteVirtualRepositoryNodePath(repo.RootPath, node.Repository.RelativePath)
	quotedTarget := remoteShellQuote(target)
	root := remoteShellQuote(repo.RootPath)
	if _, err := runRemoteRepositoryCommand(ctx, client, "root=$(realpath "+root+") || exit 21; target="+quotedTarget+"; [ -L \"$target\" ] && [ ! -e \"$target\" ] && exit 24; ancestor=\"$target\"; while [ ! -e \"$ancestor\" ]; do next=$(dirname -- \"$ancestor\"); [ \"$next\" = \"$ancestor\" ] && exit 23; ancestor=\"$next\"; done; realancestor=$(realpath \"$ancestor\") || exit 22; case \"$realancestor\" in \"$root\"|\"$root\"/*) :;; *) exit 23;; esac"); err != nil {
		return errors.New("checkout target escapes the remote virtual repository root")
	}
	if _, err := runRemoteRepositoryCommand(ctx, client, "test ! -e "+quotedTarget+" || { test -d "+quotedTarget+" && test -z \"$(ls -A "+quotedTarget+")\"; }"); err != nil {
		return errors.New("checkout target already exists and is not empty")
	}
	credential, secret, err := a.repositoryCredentialForNode(repo.ID, node.ID, node.Repository.Kind, node.Repository.RemoteURL)
	if err != nil {
		return err
	}
	if node.Repository.Kind == "git" {
		prefix, cleanup, err := remoteGitCredentialPrefix(client, credential, secret)
		if err != nil {
			return err
		}
		defer cleanup()
		command := prefix + "git clone "
		if node.Repository.RefName != "" {
			command += "--branch " + remoteShellQuote(node.Repository.RefName) + " --single-branch "
		}
		_, err = runRemoteRepositoryCommand(ctx, client, command+remoteShellQuote(sanitizeRepositoryRemoteURL(node.Repository.RemoteURL))+" "+quotedTarget)
		return err
	}
	command := "svn checkout " + remoteShellQuote(svnRepositoryURLForBinding(node.Repository)) + " " + quotedTarget + " --non-interactive --no-auth-cache "
	stdin := ""
	if credential != nil {
		help, helpErr := runRemoteRepositoryCommand(ctx, client, "svn help checkout")
		if helpErr != nil || !strings.Contains(help, "--password-from-stdin") {
			return &virtualRepositoryOperationError{Code: "credential_unavailable", Err: errors.New("remote SVN client does not support secure password input")}
		}
		command += "--username " + remoteShellQuote(credential.Username) + " --password-from-stdin"
		stdin = secret + "\n"
	}
	_, err = runRemoteRepositoryCommandInput(ctx, client, command, stdin)
	return err
}

func remoteGitCredentialPrefix(client *ssh.Client, credential *RepositoryCredentialMetadata, secret string) (string, func(), error) {
	if credential == nil {
		return "GIT_TERMINAL_PROMPT=0 ", func() {}, nil
	}
	askPass, err := createRemoteGitAskPass(client, credential.Username, secret)
	if err != nil {
		return "", nil, err
	}
	prefix := "GIT_TERMINAL_PROMPT=0 GIT_ASKPASS=" + remoteShellQuote(askPass.scriptPath) + " GIT_ASKPASS_REQUIRE=force MACLAW_VREPO_GIT_USER_FILE=" + remoteShellQuote(askPass.usernamePath) + " MACLAW_VREPO_GIT_SECRET_FILE=" + remoteShellQuote(askPass.secretPath) + " "
	return prefix, askPass.cleanup, nil
}

func (a *App) GetRemoteVirtualRepositoryDirectoryStats(repositoryID, relative string) (string, error) {
	if err := validateRemoteVirtualRepositoryRelativePath(relative); err != nil {
		return "", err
	}
	repo, client, err := a.remoteVirtualRepositoryByID(repositoryID)
	if err != nil {
		return "", err
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	target := remoteVirtualRepositoryNodePath(repo.RootPath, relative)
	command := "root=$(realpath " + remoteShellQuote(repo.RootPath) + ") || exit 21; target=$(realpath " + remoteShellQuote(target) + ") || exit 22; case \"$target\" in \"$root\"/*) find \"$target\" -type f -printf '%s\\n' 2>/dev/null | awk '{n++; s+=$1} END {printf \"%d %d\\n\", n, s}' ;; *) exit 23;; esac"
	out, err := runRemoteRepositoryCommand(ctx, client, command)
	if err != nil {
		return "", err
	}
	count, size, err := parseRemoteVirtualRepositoryDirectoryStats(out)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(VirtualRepositoryDirectoryStats{Path: target, FileCount: count, SizeBytes: size})
	return string(data), err
}

func parseRemoteVirtualRepositoryDirectoryStats(out string) (int64, int64, error) {
	var count, size int64
	var trailing string
	parsed, scanErr := fmt.Sscanf(strings.TrimSpace(out), "%d %d %s", &count, &size, &trailing)
	if scanErr != io.EOF || parsed != 2 || count < 0 || size < 0 {
		return 0, 0, fmt.Errorf("parse remote directory statistics: unexpected output %q", truncateVirtualRepositoryDiagnostic(out, 256))
	}
	return count, size, nil
}

func (a *App) executeRemoteVirtualRepositoryOperation(parent context.Context, repo *VirtualRepository, node VirtualRepositoryNode, req VirtualRepositoryOperationRequest) (string, error) {
	remoteRepo, client, err := a.remoteVirtualRepositoryByID(repo.ID)
	if err != nil {
		return "", err
	}
	defer client.Close()
	return a.executeRemoteVirtualRepositoryOperationWithClient(parent, client, remoteRepo, node, req)
}

func (a *App) executeRemoteVirtualRepositoryOperationWithClient(parent context.Context, client *ssh.Client, repo *VirtualRepository, node VirtualRepositoryNode, req VirtualRepositoryOperationRequest) (string, error) {
	if err := validateVirtualRepositoryOperationForBinding(node.Repository, req.Action); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()
	status := inspectRemoteVirtualRepositoryNode(ctx, client, repo, node)
	if status.Error != "" {
		return "", errors.New(status.Error)
	}
	workingDir := remoteShellQuote(status.Path)
	credential, secret, err := a.repositoryCredentialForNode(repo.ID, node.ID, node.Repository.Kind, node.Repository.RemoteURL)
	if err != nil {
		return "", err
	}
	if node.Repository.Kind == "git" {
		var commands []string
		gitPrefix := "GIT_TERMINAL_PROMPT=0 "
		cleanup := func() {}
		if credential != nil {
			askPass, createErr := createRemoteGitAskPass(client, credential.Username, secret)
			if createErr != nil {
				return "", &virtualRepositoryOperationError{Code: "credential_unavailable", Err: createErr}
			}
			cleanup = askPass.cleanup
			gitPrefix += "GIT_ASKPASS=" + remoteShellQuote(askPass.scriptPath) + " GIT_ASKPASS_REQUIRE=force MACLAW_VREPO_GIT_USER_FILE=" + remoteShellQuote(askPass.usernamePath) + " MACLAW_VREPO_GIT_SECRET_FILE=" + remoteShellQuote(askPass.secretPath) + " "
		}
		defer cleanup()
		switch req.Action {
		case "commit", "commit_push":
			commands = append(commands, "git -C "+workingDir+" add -A", "if git -C "+workingDir+" diff --cached --quiet; then echo 'nothing to commit' >&2; exit 44; fi", "git -C "+workingDir+" commit -m "+remoteShellQuote(req.Message))
			if req.Action == "commit_push" {
				commitOutput, commitErr := runRemoteRepositoryCommand(ctx, client, strings.Join(commands, " && "))
				if commitErr != nil {
					return commitOutput, commitErr
				}
				pushOutput, pushErr := runRemoteRepositoryCommand(ctx, client, gitPrefix+"git -C "+workingDir+" push "+remoteShellQuote(sanitizeRepositoryRemoteURL(node.Repository.RemoteURL)))
				output := strings.TrimSpace(strings.Join([]string{commitOutput, pushOutput}, "\n"))
				if pushErr != nil {
					return output, &virtualRepositoryOperationError{Code: "push_failed_after_commit", Err: fmt.Errorf("commit succeeded but push failed: %w", pushErr)}
				}
				return output, nil
			}
		case "push":
			commands = append(commands, gitPrefix+"git -C "+workingDir+" push "+remoteShellQuote(sanitizeRepositoryRemoteURL(node.Repository.RemoteURL)))
		case "revert":
			commands = append(commands, "git -C "+workingDir+" restore --staged --worktree -- . || { git -C "+workingDir+" reset --mixed HEAD -- && git -C "+workingDir+" checkout -- .; }")
		}
		return runRemoteRepositoryCommand(ctx, client, strings.Join(commands, " && "))
	}
	switch req.Action {
	case "push":
		return "SVN commit already uploads changes; push skipped", nil
	case "commit", "commit_push":
		command := "svn commit -m " + remoteShellQuote(req.Message) + " --non-interactive --no-auth-cache "
		stdin := ""
		if credential != nil {
			help, helpErr := runRemoteRepositoryCommand(ctx, client, "svn help commit")
			if helpErr != nil || !strings.Contains(help, "--password-from-stdin") {
				return "", &virtualRepositoryOperationError{Code: "credential_unavailable", Err: errors.New("remote SVN client does not support secure password input; upgrade SVN or configure its credential cache")}
			}
			command += "--username " + remoteShellQuote(credential.Username) + " --password-from-stdin "
			stdin = secret + "\n"
		}
		return runRemoteRepositoryCommandInput(ctx, client, command+workingDir, stdin)
	case "revert":
		return runRemoteRepositoryCommand(ctx, client, "svn revert -R "+workingDir)
	default:
		return "", errors.New("unsupported remote SVN operation")
	}
}
