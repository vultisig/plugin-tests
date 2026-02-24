package worker

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDnsLabel(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple lowercase", "hello", "hello"},
		{"mixed case with dash", "Hello-World", "hello-world"},
		{"strips invalid chars", "ABC_DEF!@#123", "abcdef123"},
		{"empty string", "", "x"},
		{"all dashes", "---", "x"},
		{"leading trailing dashes", "-abc-", "abc"},
		{"truncates to 63", strings.Repeat("a", 100), strings.Repeat("a", 63)},
		{"truncate then trim trailing dash", strings.Repeat("a", 62) + "-z", strings.Repeat("a", 62)},
		{"trailing dash after truncate", strings.Repeat("a", 63) + "-", strings.Repeat("a", 63)},
		{"unicode stripped", "café", "caf"},
		{"numbers allowed", "test-123-run", "test-123-run"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := dnsLabel(tt.input)
			assert.Equal(t, tt.expected, result)
			assert.LessOrEqual(t, len(result), maxDNSLabelLen)
		})
	}
}

func TestRunIDPrefix(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"standard uuid", "550e8400-e29b-41d4-a716-446655440000", "550e8400e29b"},
		{"empty string", "", "unknown"},
		{"short string", "short", "short"},
		{"exact 12 chars", "abcdefghijkl", "abcdefghijkl"},
		{"over 12 chars", "abcdefghijklmnop", "abcdefghijkl"},
		{"uppercase", "ABC-DEF", "abcdef"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runIDPrefix(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNamespaceName(t *testing.T) {
	tests := []struct {
		name     string
		runID    string
		expected string
	}{
		{"standard uuid", "550e8400-e29b-41d4-a716-446655440000", "plugin-test-550e8400e29b"},
		{"empty run id", "", "plugin-test-unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := namespaceName(tt.runID)
			assert.Equal(t, tt.expected, result)
			assert.LessOrEqual(t, len(result), maxDNSLabelLen)
		})
	}
}

func TestJobName(t *testing.T) {
	tests := []struct {
		name     string
		runID    string
		expected string
	}{
		{"standard uuid", "550e8400-e29b-41d4-a716-446655440000", "dummy-550e8400e29b"},
		{"empty run id", "", "dummy-unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := jobName(tt.runID)
			assert.Equal(t, tt.expected, result)
			assert.LessOrEqual(t, len(result), maxDNSLabelLen)
		})
	}
}

func TestNamespaceNameWithPlugin(t *testing.T) {
	tests := []struct {
		name     string
		runID    string
		pluginID string
		expected string
	}{
		{"standard", "550e8400-e29b-41d4-a716-446655440000", "vultisig-dca-0000", "plugin-test-vultisig-dca-0000-550e8400e29b"},
		{"long plugin id", "550e8400-e29b-41d4-a716-446655440000", "vultisig-recurring-sends-really-long-name-0000", "plugin-test-vultisig-recurring-sends-really-long-n-550e8400e29b"},
		{"empty run id", "", "vultisig-dca-0000", "plugin-test-vultisig-dca-0000-unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := namespaceNameWithPlugin(tt.runID, tt.pluginID)
			assert.Equal(t, tt.expected, result)
			assert.LessOrEqual(t, len(result), maxDNSLabelLen)
		})
	}
}

func TestSeederJobName(t *testing.T) {
	result := seederJobName("550e8400-e29b-41d4-a716-446655440000")
	assert.Equal(t, "seeder-550e8400e29b", result)
	assert.LessOrEqual(t, len(result), maxDNSLabelLen)
}

func TestSmokeJobName(t *testing.T) {
	result := smokeJobName("550e8400-e29b-41d4-a716-446655440000")
	assert.Equal(t, "smoke-550e8400e29b", result)
	assert.LessOrEqual(t, len(result), maxDNSLabelLen)
}

func TestRunLabels(t *testing.T) {
	labels := runLabels("some-run-id", "my-plugin", "dummy")

	assert.Len(t, labels, 4)
	assert.Equal(t, managedByValue, labels[labelManagedBy])
	assert.Equal(t, "some-run-id", labels[labelRunID])
	assert.Equal(t, "my-plugin", labels[labelPluginID])
	assert.Equal(t, "dummy", labels[labelKind])
}
