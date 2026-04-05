// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package cmd

import (
	"strings"
	"testing"
)

func TestRenderGitHubEnvUsesHeredocFormat(t *testing.T) {
	values := map[string]string{
		"MULTILINE": "line1\nline2",
		"PLAIN":     "value",
	}

	rendered := renderGitHubEnv([]string{"MULTILINE", "PLAIN"}, values)

	if !strings.Contains(rendered, "MULTILINE<<ENVSYNC_EOF\nline1\nline2\nENVSYNC_EOF\n") {
		t.Fatalf("expected heredoc format for multiline value, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "PLAIN<<ENVSYNC_EOF\nvalue\nENVSYNC_EOF\n") {
		t.Fatalf("expected heredoc format for plain value, got:\n%s", rendered)
	}
}

func TestGitHubDelimiterAvoidsValueCollision(t *testing.T) {
	delimiter := githubDelimiter("value\nENVSYNC_EOF\nmore")
	if delimiter == "ENVSYNC_EOF" {
		t.Fatalf("expected delimiter to change when value contains default marker")
	}
}
