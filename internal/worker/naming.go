package worker

import (
	"strings"
)

const (
	maxDNSLabelLen = 63
	runIDPrefixLen = 12

	labelManagedBy = "app.kubernetes.io/managed-by"
	labelRunID     = "plugin-tests/run-id"
	labelPluginID  = "plugin-tests/plugin-id"
	labelKind      = "plugin-tests/kind"

	managedByValue  = "plugin-tests"
	kindDummy       = "dummy"
	kindIntegration = "integration"

	labelComponent = "plugin-tests/component"
)

func namespaceName(runID string) string {
	return dnsLabel("plugin-test-" + runIDPrefix(runID))
}

func namespaceNameWithPlugin(runID, pluginID string) string {
	plugin := dnsLabel(pluginID)
	maxPluginLen := maxDNSLabelLen - len("plugin-test--") - runIDPrefixLen
	if len(plugin) > maxPluginLen {
		plugin = plugin[:maxPluginLen]
	}
	plugin = strings.TrimRight(plugin, "-")
	return dnsLabel("plugin-test-" + plugin + "-" + runIDPrefix(runID))
}

func jobName(runID string) string {
	return dnsLabel("dummy-" + runIDPrefix(runID))
}

func seederJobName(runID string) string {
	return dnsLabel("seeder-" + runIDPrefix(runID))
}

func smokeJobName(runID string) string {
	return dnsLabel("smoke-" + runIDPrefix(runID))
}

func integrationJobName(runID string) string {
	return dnsLabel("integration-" + runIDPrefix(runID))
}

func runLabels(runID, pluginID, kind string) map[string]string {
	return map[string]string{
		labelManagedBy: managedByValue,
		labelRunID:     runID,
		labelPluginID:  dnsLabel(pluginID),
		labelKind:      kind,
	}
}

func runIDPrefix(id string) string {
	clean := strings.ReplaceAll(id, "-", "")
	clean = strings.ToLower(clean)
	if clean == "" {
		return "unknown"
	}
	if len(clean) > runIDPrefixLen {
		return clean[:runIDPrefixLen]
	}
	return clean
}

func dnsLabel(s string) string {
	s = strings.ToLower(s)

	var b strings.Builder
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			b.WriteRune(c)
		}
	}
	result := b.String()

	if len(result) > maxDNSLabelLen {
		result = result[:maxDNSLabelLen]
	}

	result = strings.Trim(result, "-")
	if result == "" {
		return "x"
	}
	return result
}
