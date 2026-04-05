// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package relay

import (
	"encoding/json"
	"fmt"
)

// TierStatus describes a team's current tier and usage.
type TierStatus struct {
	TeamID             string     `json:"team_id"`
	Tier               string     `json:"tier"`
	StripeSubscription string     `json:"stripe_subscription"`
	Usage              TierUsage  `json:"usage"`
	Limits             TierLimits `json:"limits"`
}

// TierUsage tracks current usage.
type TierUsage struct {
	Members    int `json:"members"`
	BlobsToday int `json:"blobs_today"`
}

// TierLimits describes the limits for a tier.
type TierLimits struct {
	Members     int `json:"members"`       // -1 = unlimited
	BlobsPerDay int `json:"blobs_per_day"` // -1 = unlimited
	HistoryDays int `json:"history_days"`
}

// GetTierStatus retrieves the current tier and usage for a team.
func (c *Client) GetTierStatus(teamID string) (*TierStatus, error) {
	path := fmt.Sprintf("/billing/status/%s", teamID)
	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, readError(resp)
	}

	var status TierStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, err
	}
	return &status, nil
}

// IsUnlimited returns true if the limit value means unlimited.
func IsUnlimited(limit int) bool {
	return limit < 0
}

// FormatLimit formats a limit for display.
func FormatLimit(limit int) string {
	if limit < 0 {
		return "unlimited"
	}
	return fmt.Sprintf("%d", limit)
}
