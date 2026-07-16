package lansenger

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// groupInfoFetchConcurrency bounds parallel GetGroupInfo calls when listing
// joined groups. Keep modest to avoid hammering enterprise gateways.
const groupInfoFetchConcurrency = 8

// maxJoinedGroupsListed caps how many groups we enrich with GetGroupInfo so the
// settings dialog stays responsive on bots that belong to very large orgs.
const maxJoinedGroupsListed = 300

// GroupInfo is the detailed group payload from /v2/groups/{id}/info/fetch.
type GroupInfo struct {
	GroupID      string `json:"group_id"`
	Name         string `json:"name"`
	AvatarURL    string `json:"avatar_url,omitempty"`
	Description  string `json:"description,omitempty"`
	OwnerID      string `json:"owner_id,omitempty"`
	OwnerName    string `json:"owner_name,omitempty"`
	State        int    `json:"state"` // 0=normal, 1=disbanded
	TotalMembers int    `json:"total_members"`
	MaxMembers   int    `json:"max_members,omitempty"`
	IsPublic     bool   `json:"is_public,omitempty"`
}

// GroupListResult is the full list of groups the bot has joined, optionally
// enriched with per-group details.
type GroupListResult struct {
	Total  int         `json:"total"`
	Groups []GroupInfo `json:"groups"`
}

type groupStaffRef struct {
	StaffID string `json:"staffId"`
	Name    string `json:"name"`
}

// QueryGroups lists group IDs the bot has joined (paginated).
// API: GET /v2/groups/fetch
func (g *Gateway) QueryGroups(ctx context.Context, pageOffset, pageSize int) (total int, groupIDs []string, err error) {
	total, groupIDs, err = g.queryGroupsOnce(ctx, pageOffset, pageSize)
	if err != nil && isLansengerTokenExpiredError(err) {
		g.tokens.clear()
		return g.queryGroupsOnce(ctx, pageOffset, pageSize)
	}
	return total, groupIDs, err
}

func (g *Gateway) queryGroupsOnce(ctx context.Context, pageOffset, pageSize int) (total int, groupIDs []string, err error) {
	if pageOffset < 0 {
		pageOffset = 0
	}
	if pageSize <= 0 {
		pageSize = 100
	}
	if pageSize > 100 {
		pageSize = 100
	}
	token, err := g.getAppToken(ctx)
	if err != nil {
		return 0, nil, err
	}
	endpoint := fmt.Sprintf("%s?app_token=%s&page_offset=%d&page_size=%d",
		g.apiURL("/v2/groups/fetch"),
		url.QueryEscape(token),
		pageOffset,
		pageSize,
	)
	var result struct {
		ErrCode int    `json:"errCode"`
		ErrMsg  string `json:"errMsg"`
		Data    struct {
			TotalGroupIDs int      `json:"totalGroupIds"`
			GroupIDs      []string `json:"groupIds"`
		} `json:"data"`
	}
	if err := g.doGetJSON(ctx, endpoint, &result); err != nil {
		return 0, nil, err
	}
	if result.ErrCode != 0 {
		return 0, nil, &APIError{Code: result.ErrCode, Msg: result.ErrMsg}
	}
	if result.Data.GroupIDs == nil {
		result.Data.GroupIDs = []string{}
	}
	return result.Data.TotalGroupIDs, result.Data.GroupIDs, nil
}

// GetGroupInfo fetches detailed metadata for one group.
// API: GET /v2/groups/{groupId}/info/fetch
func (g *Gateway) GetGroupInfo(ctx context.Context, groupID string) (*GroupInfo, error) {
	info, err := g.getGroupInfoOnce(ctx, groupID)
	if err != nil && isLansengerTokenExpiredError(err) {
		g.tokens.clear()
		return g.getGroupInfoOnce(ctx, groupID)
	}
	return info, err
}

func (g *Gateway) getGroupInfoOnce(ctx context.Context, groupID string) (*GroupInfo, error) {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return nil, fmt.Errorf("lansenger: groupId is required")
	}
	token, err := g.getAppToken(ctx)
	if err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("%s?app_token=%s",
		g.apiURL("/v2/groups/"+url.PathEscape(groupID)+"/info/fetch"),
		url.QueryEscape(token),
	)
	var result struct {
		ErrCode int    `json:"errCode"`
		ErrMsg  string `json:"errMsg"`
		Data    struct {
			Name         string        `json:"name"`
			AvatarURL    string        `json:"avatarUrl"`
			Description  string        `json:"description"`
			Owner        groupStaffRef `json:"owner"`
			State        int           `json:"state"`
			TotalMembers int           `json:"totalMembers"`
			MaxMembers   int           `json:"maxMembers"`
			IsPublic     bool          `json:"isPublic"`
		} `json:"data"`
	}
	if err := g.doGetJSON(ctx, endpoint, &result); err != nil {
		return nil, err
	}
	if result.ErrCode != 0 {
		return nil, &APIError{Code: result.ErrCode, Msg: result.ErrMsg}
	}
	info := &GroupInfo{
		GroupID:      groupID,
		Name:         strings.TrimSpace(result.Data.Name),
		AvatarURL:    strings.TrimSpace(result.Data.AvatarURL),
		Description:  strings.TrimSpace(result.Data.Description),
		OwnerID:      strings.TrimSpace(result.Data.Owner.StaffID),
		OwnerName:    strings.TrimSpace(result.Data.Owner.Name),
		State:        result.Data.State,
		TotalMembers: result.Data.TotalMembers,
		MaxMembers:   result.Data.MaxMembers,
		IsPublic:     result.Data.IsPublic,
	}
	if info.Name == "" {
		info.Name = groupID
	}
	return info, nil
}

// ListJoinedGroups pages through all groups the bot has joined and enriches
// each entry with GetGroupInfo (bounded concurrency). Failures fetching
// individual group details degrade to a GroupInfo containing only the group ID.
func (g *Gateway) ListJoinedGroups(ctx context.Context) (*GroupListResult, error) {
	ids, reportedTotal, err := g.listAllGroupIDs(ctx)
	if err != nil {
		return nil, err
	}
	groups := g.fetchGroupInfos(ctx, ids)
	if reportedTotal == 0 {
		reportedTotal = len(groups)
	}
	if groups == nil {
		groups = []GroupInfo{}
	}
	return &GroupListResult{Total: reportedTotal, Groups: groups}, nil
}

func (g *Gateway) listAllGroupIDs(ctx context.Context) ([]string, int, error) {
	const pageSize = 100
	// Hard cap prevents unbounded loops if the API returns inconsistent totals.
	const maxPages = 50
	offset := 0
	var allIDs []string
	reportedTotal := 0
	for page := 0; page < maxPages; page++ {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		total, ids, err := g.QueryGroups(ctx, offset, pageSize)
		if err != nil {
			return nil, 0, err
		}
		if reportedTotal == 0 {
			reportedTotal = total
		}
		allIDs = append(allIDs, ids...)
		// Stop paging once we have enough IDs for the UI cap; still keep the
		// server-reported total so the dialog can show "loaded N of M".
		if len(allIDs) >= maxJoinedGroupsListed {
			break
		}
		if len(ids) == 0 || len(allIDs) >= total || len(ids) < pageSize {
			break
		}
		offset += len(ids)
	}
	allIDs = dedupePreserveOrder(allIDs)
	if len(allIDs) > maxJoinedGroupsListed {
		allIDs = allIDs[:maxJoinedGroupsListed]
	}
	if reportedTotal < len(allIDs) {
		reportedTotal = len(allIDs)
	}
	return allIDs, reportedTotal, nil
}

func dedupePreserveOrder(ids []string) []string {
	if len(ids) == 0 {
		return ids
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func (g *Gateway) fetchGroupInfos(ctx context.Context, ids []string) []GroupInfo {
	if len(ids) == 0 {
		return []GroupInfo{}
	}
	// Warm the token cache once so N parallel workers do not stampede create.
	if _, err := g.getAppToken(ctx); err != nil {
		// Fall through: each GetGroupInfo will surface the same error as degrade.
		log.Printf("[lansenger] prewarm app token for group list: %v", err)
	}
	groups := make([]GroupInfo, len(ids))
	workers := groupInfoFetchConcurrency
	if workers > len(ids) {
		workers = len(ids)
	}
	jobs := make(chan int, workers)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				id := ids[i]
				if ctx.Err() != nil {
					groups[i] = GroupInfo{GroupID: id, Name: id}
					continue
				}
				info, err := g.GetGroupInfo(ctx, id)
				if err != nil {
					log.Printf("[lansenger] GetGroupInfo %s: %v", id, err)
					groups[i] = GroupInfo{GroupID: id, Name: id}
					continue
				}
				groups[i] = *info
			}
		}()
	}
	for i := range ids {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return groups
}

func (g *Gateway) doGetJSON(ctx context.Context, endpoint string, out any) error {
	var lastErr error
	for attempt := 1; attempt <= lansengerAPIMaxRetry; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		reqStart := time.Now()
		resp, err := g.client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			lastErr = err
			log.Printf("[lansenger] error: API GET failed (attempt %d/%d, took %v): %v", attempt, lansengerAPIMaxRetry, time.Since(reqStart), err)
			if attempt < lansengerAPIMaxRetry {
				apiRetryBackoff(ctx, attempt)
				continue
			}
			return err
		}
		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			if attempt < lansengerAPIMaxRetry {
				apiRetryBackoff(ctx, attempt)
				continue
			}
			return readErr
		}
		if resp.StatusCode != http.StatusOK {
			err := fmt.Errorf("lansenger API HTTP %d: %s", resp.StatusCode, string(respBody))
			lastErr = err
			if isRetryableHTTPStatus(resp.StatusCode) && attempt < lansengerAPIMaxRetry {
				apiRetryBackoff(ctx, attempt)
				continue
			}
			return err
		}
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("lansenger: decode response: %w", err)
		}
		return nil
	}
	return lastErr
}
