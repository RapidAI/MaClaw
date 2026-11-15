package desktoppool

import (
	"context"
	"errors"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func TestFitNodeResourcesSizesAnInstalledDesktop(t *testing.T) {
	pool := New(&memSettings{}, memDir{})
	ctx := context.Background()
	owned, err := pool.AttachOwnedServer(ctx, "tenant-a", "alice", Server{
		Name: "lab", BaseURL: NodeBaseURL("dn_lab"), AccessToken: "desk-token", NodeID: "dn_lab",
	})
	if err != nil {
		t.Fatal(err)
	}
	if owned.Memory != desktop.DefaultMemory || owned.CPUs != desktop.DefaultCPUs {
		t.Fatalf("first record = %+v", owned)
	}
	if err := pool.FitNodeResources(ctx, "tenant-a", "dn_lab", 20<<30, 8); err != nil {
		t.Fatal(err)
	}
	servers, _, err := pool.OwnedServers(ctx, "tenant-a", "alice")
	if err != nil || len(servers) != 1 || servers[0].Memory != "4g" || servers[0].CPUs != "4" || servers[0].ShmSize != "1g" || !servers[0].ResourcesAuto {
		t.Fatalf("fitted = %+v err %v", servers, err)
	}
	if err := pool.FitNodeResources(ctx, "tenant-a", "dn_lab", 40<<30, 8); err != nil {
		t.Fatal(err)
	}
	servers, _, _ = pool.OwnedServers(ctx, "tenant-a", "alice")
	if servers[0].Memory != "6g" || servers[0].ShmSize != "2g" {
		t.Fatalf("refit = %+v", servers[0])
	}
	if err := pool.FitNodeResources(ctx, "tenant-a", "dn_lab", 4<<30, 2); !errors.Is(err, desktop.ErrHostTooSmall) {
		t.Fatalf("small err = %v", err)
	}
	servers, _, _ = pool.OwnedServers(ctx, "tenant-a", "alice")
	if servers[0].Memory != "6g" {
		t.Fatalf("small machine changed the desktop: %+v", servers[0])
	}
}

func TestFitNodeResourcesLeavesAHandSetSizeAndACloudDesktop(t *testing.T) {
	pool := New(&memSettings{}, memDir{})
	ctx := context.Background()
	cloud, err := pool.CreateServer(ctx, "tenant-a", Server{
		Name: "cloud", BaseURL: "http://cloud.example", AccessToken: "cloud-token", NodeID: "cloud-node",
		Memory: "8g", CPUs: "2", ShmSize: "1g",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.FitNodeResources(ctx, "tenant-a", "cloud-node", 40<<30, 8); err != nil {
		t.Fatal(err)
	}
	view, err := pool.View(ctx, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	for _, server := range view.Servers {
		if server.ID == cloud.ID && (server.Memory != "8g" || server.ResourcesAuto) {
			t.Fatalf("cloud changed: %+v", server)
		}
	}
	if err := pool.SetNodeResources(ctx, "tenant-a", "cloud-node", "3g", "1.5", "1g"); !errors.Is(err, ErrDesktopNotRecorded) {
		t.Fatalf("cloud pin = %v", err)
	}
	view, err = pool.View(ctx, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	for _, server := range view.Servers {
		if server.ID == cloud.ID && server.Memory != "8g" {
			t.Fatalf("cloud pin changed it: %+v", server)
		}
	}
	owned, err := pool.AttachOwnedServer(ctx, "tenant-a", "alice", Server{
		Name: "lab", BaseURL: NodeBaseURL("dn_lab"), AccessToken: "desk-token", NodeID: "dn_lab",
		Memory: "8g", CPUs: "2", ShmSize: "1g",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.FitNodeResources(ctx, "tenant-a", "dn_lab", 40<<30, 8); err != nil {
		t.Fatal(err)
	}
	servers, _, _ := pool.OwnedServers(ctx, "tenant-a", "alice")
	if len(servers) != 1 || servers[0].ID != owned.ID || servers[0].Memory != "8g" || servers[0].ResourcesAuto {
		t.Fatalf("hand-set size changed: %+v", servers)
	}
	if err := pool.SetNodeResources(ctx, "tenant-a", "dn_lab", "5g", "2", "1g"); err != nil {
		t.Fatal(err)
	}
	if err := pool.FitNodeResources(ctx, "tenant-a", "dn_lab", 40<<30, 8); err != nil {
		t.Fatal(err)
	}
	servers, _, _ = pool.OwnedServers(ctx, "tenant-a", "alice")
	if servers[0].Memory != "5g" || servers[0].CPUs != "2" || !servers[0].ResourcesPinned || servers[0].ResourcesAuto {
		t.Fatalf("typed size was replaced: %+v", servers[0])
	}
	if err := pool.AllowNodeRefit(ctx, "tenant-a", "dn_lab"); err != nil {
		t.Fatal(err)
	}
	if err := pool.FitNodeResources(ctx, "tenant-a", "dn_lab", 40<<30, 8); err != nil {
		t.Fatal(err)
	}
	servers, _, _ = pool.OwnedServers(ctx, "tenant-a", "alice")
	if servers[0].Memory != "6g" || servers[0].ShmSize != "2g" || !servers[0].ResourcesAuto || servers[0].ResourcesPinned {
		t.Fatalf("restore did not measure again: %+v", servers[0])
	}
}

func TestUpdateServerPinsATypedSize(t *testing.T) {
	pool := New(&memSettings{}, memDir{})
	ctx := context.Background()
	owned, err := pool.AttachOwnedServer(ctx, "tenant-a", "alice", Server{
		Name: "lab", BaseURL: NodeBaseURL("dn_lab"), AccessToken: "desk-token", NodeID: "dn_lab",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.FitNodeResources(ctx, "tenant-a", "dn_lab", 20<<30, 8); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.UpdateServer(ctx, "tenant-a", owned.ID, Server{
		Name: "lab-renamed", BaseURL: NodeBaseURL("dn_lab"), AccessToken: "desk-token",
		Memory: "4g", CPUs: "4", ShmSize: "1g", NodeID: "dn_lab",
	}, true); err != nil {
		t.Fatal(err)
	}
	if err := pool.FitNodeResources(ctx, "tenant-a", "dn_lab", 40<<30, 8); err != nil {
		t.Fatal(err)
	}
	servers, _, err := pool.OwnedServers(ctx, "tenant-a", "alice")
	if err != nil || len(servers) != 1 || servers[0].Memory != "6g" || !servers[0].ResourcesAuto || servers[0].ResourcesPinned {
		t.Fatalf("renaming pinned the measured size: %+v err %v", servers, err)
	}
	if _, err := pool.UpdateServer(ctx, "tenant-a", owned.ID, Server{
		Name: "lab-renamed", BaseURL: NodeBaseURL("dn_lab"), AccessToken: "desk-token",
		Memory: "3g", CPUs: "1.5", ShmSize: "1g", NodeID: "dn_lab",
	}, true); err != nil {
		t.Fatal(err)
	}
	if err := pool.FitNodeResources(ctx, "tenant-a", "dn_lab", 40<<30, 8); err != nil {
		t.Fatal(err)
	}
	servers, _, _ = pool.OwnedServers(ctx, "tenant-a", "alice")
	if servers[0].Memory != "3g" || servers[0].CPUs != "1.5" || !servers[0].ResourcesPinned || servers[0].ResourcesAuto {
		t.Fatalf("typed default was replaced: %+v", servers[0])
	}
}

func TestUpdateServerKeepsAutomaticWhenTheWrittenSizeMatches(t *testing.T) {
	pool := New(&memSettings{}, memDir{})
	ctx := context.Background()
	owned, err := pool.AttachOwnedServer(ctx, "tenant-a", "alice", Server{
		Name: "lab", BaseURL: NodeBaseURL("dn_lab"), AccessToken: "desk-token", NodeID: "dn_lab",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.FitNodeResources(ctx, "tenant-a", "dn_lab", 20<<30, 8); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.UpdateServer(ctx, "tenant-a", owned.ID, Server{
		Name: "lab", BaseURL: NodeBaseURL("dn_lab"), AccessToken: "desk-token",
		Memory: "4G", CPUs: "4", ShmSize: "1G", NodeID: "dn_lab",
	}, true); err != nil {
		t.Fatal(err)
	}
	if err := pool.FitNodeResources(ctx, "tenant-a", "dn_lab", 40<<30, 8); err != nil {
		t.Fatal(err)
	}
	servers, _, err := pool.OwnedServers(ctx, "tenant-a", "alice")
	if err != nil || len(servers) != 1 || servers[0].Memory != "6g" || !servers[0].ResourcesAuto || servers[0].ResourcesPinned {
		t.Fatalf("writing the same size pinned it: %+v err %v", servers, err)
	}
}
