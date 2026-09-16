package policy

import (
	"path"
	"strings"

	"apihub-go/internal/model"
)

func AllowsAction(token model.RuntimeToken, actionID string) bool {
	if matches(token.BlockedActions, actionID) {
		return false
	}
	return matches(token.AllowedActions, actionID)
}

func AllowsConnection(token model.RuntimeToken, connection model.Connection) bool {
	if len(token.AllowedConnections) == 0 {
		return true
	}
	return matches(token.AllowedConnections, connection.ID) ||
		matches(token.AllowedConnections, connection.ConnectionKey) ||
		matches(token.AllowedConnections, connection.IntegrationKey+":"+connection.Name)
}

func matches(patterns []string, value string) bool {
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		matched, err := path.Match(pattern, value)
		if err == nil && matched {
			return true
		}
	}
	return false
}
