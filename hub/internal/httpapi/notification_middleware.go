package httpapi

import (
	"context"
	"net/http"
)

// machineIDContextKey is the context key for storing the authenticated machine ID.
type machineIDContextKey struct{}

// MachineIDFromContext retrieves the authenticated machine ID from the request context.
func MachineIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(machineIDContextKey{}).(string)
	return v
}

// requireMachineAuth wraps a handler with machine token authentication.
// It authenticates the machine using the identity service and stores the machine ID
// in the request context for downstream handlers via MachineIDFromContext.
func requireMachineAuth(identity veMachineAuthenticator) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			principal, ok := authenticateVEMachine(w, r, identity)
			if !ok {
				return
			}
			ctx := context.WithValue(r.Context(), machineIDContextKey{}, principal.MachineID)
			next(w, r.WithContext(ctx))
		}
	}
}
