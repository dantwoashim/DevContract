// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package relay

import (
	"encoding/json"
	"fmt"
)

// LimitsStatus describes the relay policy and current usage for a team.
type LimitsStatus struct {
	TeamID             string      `json:"team_id"`
	Tier               string      `json:"tier"`
	StripeSubscription string      `json:"stripe_subscription"`
	Usage              RelayUsage  `json:"usage"`
	Limits             RelayLimits `json:"limits"`
}

// RelayUsage tracks current relay-side usage.
type RelayUsage struct {
	Members    int `json:"members"`
	BlobsToday int `json:"blobs_today"`
}

// RelayLimits describes the deployment-configured relay limits.
type RelayLimits struct {
	Members     int `json:"members"`       // -1 = unlimited
	BlobsPerDay int `json:"blobs_per_day"` // -1 = unlimited
	HistoryDays int `json:"history_days"`
}

// TierStatus is retained as a compatibility alias for older callers.
type TierStatus = LimitsStatus

// TierUsage is retained as a compatibility alias for older callers.
type TierUsage = RelayUsage

// TierLimits is retained as a compatibility alias for older callers.
type TierLimits = RelayLimits

// GetLimitsStatus retrieves the current relay limits and usage for a team.
func (c *Client) GetLimitsStatus(teamID string) (*LimitsStatus, error) {
	path := fmt.Sprintf("/teams/%s/limits", teamID)
	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, readError(resp)
	}

	var status LimitsStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, err
	}
	return &status, nil
}

// GetTierStatus is retained as a compatibility alias for older callers.
func (c *Client) GetTierStatus(teamID string) (*LimitsStatus, error) {
	return c.GetLimitsStatus(teamID)
}
