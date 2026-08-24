package tool

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// When a dispatch lease lapses the operation converges to unknown, and the
// only remaining question is where the send was attempted. The claim is the
// one place that can answer it, so an unstamped claim leaves the person
// resolving the unknown with nowhere to look.
func TestAClaimRecordsWhereTheDispatchWasAttempted(t *testing.T) {
	coordinator, scope, selectionID := staleDeliveryFixture(t, "root-holder")

	var holder string
	if err := coordinator.db.QueryRow(`SELECT claim_holder FROM semantic_delivery_preparations WHERE delivery_key=?`,
		deliveryStoreKey(scope, selectionID)).Scan(&holder); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(holder) == "" {
		t.Fatal("the claim recorded no holder at all")
	}
	if holder != LocalDispatchHolder() {
		t.Fatalf("claim holder=%q want %q", holder, LocalDispatchHolder())
	}
}

// A deployment that knows its own replica identity should be able to say so,
// because its name will outlive the guess made here.
func TestADeclaredDispatchHolderIsUsedVerbatim(t *testing.T) {
	t.Setenv("MACLAW_DISPATCH_HOLDER", "  gateway-replica-3  ")
	if got := resolveLocalDispatchHolder(); got != "gateway-replica-3" {
		t.Fatalf("declared holder=%q", got)
	}
}

// Without one, the fallback still has to name a machine and a process
// lifetime: after a restart the same host is a different process, and an
// investigation needs to tell those apart.
func TestTheFallbackDispatchHolderNamesHostAndProcess(t *testing.T) {
	t.Setenv("MACLAW_DISPATCH_HOLDER", "")
	holder := resolveLocalDispatchHolder()
	host, _ := os.Hostname()
	if host != "" && !strings.HasPrefix(holder, host+":") {
		t.Fatalf("holder=%q does not name host %q", holder, host)
	}
	if !strings.HasSuffix(holder, ":"+strconv.Itoa(os.Getpid())) {
		t.Fatalf("holder=%q does not name pid %d", holder, os.Getpid())
	}
}

// Stamping the holder must not have changed what a claim is: it still excludes
// a second claimant, and it still refuses to reopen a lapsed lease.
func TestStampingTheHolderChangesNothingAboutTheClaim(t *testing.T) {
	coordinator, scope, selectionID := staleDeliveryFixture(t, "root-holder-claim")

	if _, claimed, err := coordinator.ClaimDelivery(scope, selectionID, time.Now().UTC()); err != nil || claimed {
		t.Fatalf("a second claimant got the dispatch, claimed=%v err=%v", claimed, err)
	}
	if _, err := coordinator.ReconcileStaleDeliveryDispatches(time.Now().UTC().Add(30*time.Minute), DeliveryDispatchLease); err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := coordinator.ClaimDelivery(scope, selectionID, time.Now().UTC()); err != nil || claimed {
		t.Fatalf("a lapsed lease was reopened, claimed=%v err=%v", claimed, err)
	}
}
