package httpapi

import "testing"

func TestUserRankingEmailFilterRejectsUIDs(t *testing.T) {
	for _, tc := range []struct {
		email string
		want  bool
	}{
		{email: "user@example.com", want: true},
		{email: " User@Example.com ", want: true},
		{email: "u_1774182684297100200", want: false},
		{email: "", want: false},
	} {
		if got := isUserRankingEmail(tc.email); got != tc.want {
			t.Fatalf("isUserRankingEmail(%q) = %v, want %v", tc.email, got, tc.want)
		}
	}
}

func TestSortUserRankingRowsByDuration(t *testing.T) {
	rows := []userRankingRow{
		{UserEmail: "fast@example.com", TotalTokens: 100, DurationSeconds: 60},
		{UserEmail: "slow@example.com", TotalTokens: 10, DurationSeconds: 3600},
	}
	assignUserRankingRanks(rows)
	sortUserRankingRows(rows, "duration")

	if rows[0].UserEmail != "slow@example.com" || rows[0].DurationRank != 1 || rows[0].TokenRank != 2 {
		t.Fatalf("unexpected first row: %#v", rows[0])
	}
	if rows[1].UserEmail != "fast@example.com" || rows[1].DurationRank != 2 || rows[1].TokenRank != 1 {
		t.Fatalf("unexpected second row: %#v", rows[1])
	}
}
func TestUserRankingEmailFilterRejectsMalformedEmails(t *testing.T) {
	for _, email := range []string{"foo@", "@example.com", "foo @example.com", "foo@@example.com"} {
		if isUserRankingEmail(email) {
			t.Fatalf("isUserRankingEmail(%q) = true, want false", email)
		}
	}
}
