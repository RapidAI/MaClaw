package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const creditsAssetMaxPayloadBytes = 256 << 10

// GetCreditsAssetAccount returns the signed-in user's market Credits balance.
// The session credential remains in the native process via marketRequest.
func (a *App) GetCreditsAssetAccount() (map[string]interface{}, error) {
	data, err := a.marketRequest(http.MethodGet, "/api/v1/credits/account", nil, "", creditsAssetMaxPayloadBytes, creditsAssetMaxPayloadBytes, "Credits")
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	return result, json.Unmarshal(data, &result)
}

// GetCreditsAssetTransactions returns the signed-in user's Credits ledger.
func (a *App) GetCreditsAssetTransactions(offset int) (map[string]interface{}, error) {
	if offset < 0 {
		offset = 0
	}
	path := fmt.Sprintf("/api/v1/credits/account/transactions?limit=100&offset=%d", offset)
	data, err := a.marketRequest(http.MethodGet, path, nil, "", creditsAssetMaxPayloadBytes, creditsAssetMaxPayloadBytes, "Credits")
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	return result, json.Unmarshal(data, &result)
}

// RedeemCreditsCard applies a one-time Credits card to the authenticated user.
func (a *App) RedeemCreditsCard(code string) (map[string]interface{}, error) {
	body, err := json.Marshal(map[string]string{"code": strings.TrimSpace(code)})
	if err != nil {
		return nil, err
	}
	data, err := a.marketRequest(http.MethodPost, "/api/v1/credits/redeem", bytes.NewReader(body), "application/json", creditsAssetMaxPayloadBytes, creditsAssetMaxPayloadBytes, "Credits")
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	return result, json.Unmarshal(data, &result)
}
