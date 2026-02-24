package queue

const QueueName = "plugin_test_queue"

const TypeRunIntegrationTest = "plugin_test:run"

type TestRunPayload struct {
	RunID       string `json:"run_id"`
	PluginID    string `json:"plugin_id"`
	ProposalID  string `json:"proposal_id,omitempty"`
	Version     string `json:"version,omitempty"`
	ArtifactRef string `json:"artifact_ref,omitempty"`
	Suite       string `json:"suite"`
	RequestedBy string `json:"requested_by"`

	PluginEndpoint string `json:"plugin_endpoint,omitempty"`
	PluginAPIKey   string `json:"plugin_api_key,omitempty"`
}
