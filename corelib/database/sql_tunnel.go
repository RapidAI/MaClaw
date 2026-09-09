package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net"
	"strings"

	mysql "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	mssql "github.com/microsoft/go-mssqldb"
)

func newSQLAdapterFromDB(dialect string, db *sql.DB, p Profile, secret string) *sqlAdapter {
	return &sqlAdapter{db: db, dialect: dialect, profile: p, secret: secret}
}

func openTunneledSQLAdapter(dialect, dsn string, p Profile, secret string, dial TunnelDialer) (*sqlAdapter, error) {
	if err := requireTunnelDialer(p, dial); err != nil {
		return nil, err
	}
	sessionID := strings.TrimSpace(p.SSHSessionID)
	switch dialect {
	case "mysql":
		return openTunneledMySQL(dsn, p, secret, sessionID, dial)
	case "postgres":
		return openTunneledPostgres(dsn, p, secret, sessionID, dial)
	case "sqlserver":
		return openTunneledSQLServer(dsn, p, secret, sessionID, dial)
	default:
		return nil, fmt.Errorf("unsupported_capability: ssh tunnel is not available for %s", dialect)
	}
}

func mysqlTunnelNetwork(sessionID string) string {
	sum := sha256.Sum256([]byte(sessionID))
	return "maclaw-ssh-" + hex.EncodeToString(sum[:8])
}

func openTunneledMySQL(dsn string, p Profile, secret, sessionID string, dial TunnelDialer) (*sqlAdapter, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, classify(err)
	}
	netName := mysqlTunnelNetwork(sessionID)
	mysql.RegisterDialContext(netName, func(ctx context.Context, addr string) (net.Conn, error) {
		return dial(ctx, sessionID, "tcp", addr)
	})
	cfg.Net = netName
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, classify(err)
	}
	return newSQLAdapterFromDB("mysql", db, p, secret), nil
}

func openTunneledPostgres(dsn string, p Profile, secret, sessionID string, dial TunnelDialer) (*sqlAdapter, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, classify(err)
	}
	cfg.LookupFunc = func(ctx context.Context, host string) ([]string, error) {
		return []string{host}, nil
	}
	cfg.DialFunc = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if network == "" {
			network = "tcp"
		}
		return dial(ctx, sessionID, network, addr)
	}
	return newSQLAdapterFromDB("postgres", stdlib.OpenDB(*cfg), p, secret), nil
}

type tunnelHostDialer struct {
	dial      TunnelDialer
	sessionID string
	hostName  string
}

func (d tunnelHostDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if network == "" {
		network = "tcp"
	}
	return d.dial(ctx, d.sessionID, network, addr)
}

func (d tunnelHostDialer) HostName() string { return d.hostName }

func openTunneledSQLServer(dsn string, p Profile, secret, sessionID string, dial TunnelDialer) (*sqlAdapter, error) {
	connector, err := mssql.NewConnector(dsn)
	if err != nil {
		return nil, classify(err)
	}
	hostName := strings.TrimSpace(p.Host)
	if i := strings.IndexRune(hostName, '\\'); i >= 0 {
		hostName = hostName[:i]
	}
	connector.Dialer = tunnelHostDialer{dial: dial, sessionID: sessionID, hostName: hostName}
	return newSQLAdapterFromDB("sqlserver", sql.OpenDB(connector), p, secret), nil
}
