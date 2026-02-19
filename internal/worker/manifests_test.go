package worker

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vultisig/plugin-tests/config"
)

var testK8sCfg = config.K8sJobConfig{
	EncryptionSecret: "test-secret",
	JWTSecret:        "jwt-test-secret",
	PluginEndpoint:   "https://plugin.example.com",
}

func TestInfraPostgresObjects(t *testing.T) {
	labels := map[string]string{"run": "123"}
	dep, svc := infraPostgresObjects("test-ns", "postgres:15", labels)

	assert.Equal(t, "postgres", dep.Name)
	assert.Equal(t, "test-ns", dep.Namespace)
	require.Len(t, dep.Spec.Template.Spec.Containers, 1)
	assert.Equal(t, "postgres:15", dep.Spec.Template.Spec.Containers[0].Image)
	assert.NotNil(t, dep.Spec.Template.Spec.Containers[0].ReadinessProbe)

	assert.Equal(t, "postgres", svc.Name)
	require.Len(t, svc.Spec.Ports, 1)
	assert.Equal(t, int32(5432), svc.Spec.Ports[0].Port)
}

func TestInfraRedisObjects(t *testing.T) {
	dep, svc := infraRedisObjects("test-ns", "redis:7", nil)

	assert.Equal(t, "redis", dep.Name)
	require.Len(t, dep.Spec.Template.Spec.Containers, 1)
	assert.Equal(t, "redis:7", dep.Spec.Template.Spec.Containers[0].Image)

	assert.Equal(t, "redis", svc.Name)
	require.Len(t, svc.Spec.Ports, 1)
	assert.Equal(t, int32(6379), svc.Spec.Ports[0].Port)
}

func TestInfraMinioObjects(t *testing.T) {
	dep, svc := infraMinioObjects("test-ns", "minio/minio:latest", nil)

	assert.Equal(t, "minio", dep.Name)
	require.Len(t, dep.Spec.Template.Spec.Containers, 1)
	c := dep.Spec.Template.Spec.Containers[0]
	assert.Equal(t, "minio/minio:latest", c.Image)
	assert.Equal(t, []string{"minio", "server", "/data"}, c.Command)
	require.NotNil(t, c.ReadinessProbe)
	assert.Equal(t, "/minio/health/live", c.ReadinessProbe.HTTPGet.Path)

	assert.Equal(t, "minio", svc.Name)
	require.Len(t, svc.Spec.Ports, 1)
	assert.Equal(t, int32(9000), svc.Spec.Ports[0].Port)
}

func TestVerifierConfigMap(t *testing.T) {
	cm, err := verifierConfigMap("test-ns", testK8sCfg)
	require.NoError(t, err)

	assert.Equal(t, "verifier-config", cm.Name)
	assert.Equal(t, "test-ns", cm.Namespace)

	raw := cm.Data["config.json"]
	require.NotEmpty(t, raw)

	var parsed verifierJSON
	err = json.Unmarshal([]byte(raw), &parsed)
	require.NoError(t, err)

	assert.Equal(t, "0.0.0.0", parsed.Server.Host)
	assert.Equal(t, 8080, parsed.Server.Port)
	assert.Equal(t, "jwt-test-secret", parsed.Server.JWTSecret)
	assert.Equal(t, "redis", parsed.Redis.Host)
	assert.Equal(t, "http://minio:9000", parsed.BlockStorage.Host)
	assert.Equal(t, "test-secret", parsed.EncryptionSecret)
	assert.True(t, parsed.Auth.Enabled)
}

func TestWorkerConfigMap(t *testing.T) {
	cm, err := workerConfigMap("test-ns", testK8sCfg)
	require.NoError(t, err)

	assert.Equal(t, "worker-config", cm.Name)

	raw := cm.Data["config.json"]
	require.NotEmpty(t, raw)

	var parsed workerJSON
	err = json.Unmarshal([]byte(raw), &parsed)
	require.NoError(t, err)

	assert.Equal(t, "https://api.vultisig.com/router", parsed.VaultService.Relay.Server)
	assert.Equal(t, "verifier", parsed.VaultService.LocalPartyPrefix)
	assert.Equal(t, "test-secret", parsed.VaultService.EncryptionSecret)
	assert.False(t, parsed.VaultService.DoSetupMsg)
}

func TestVerifierDeploymentObjects(t *testing.T) {
	labels := map[string]string{"run": "123"}

	t.Run("with pull secret", func(t *testing.T) {
		dep, svc := verifierDeploymentObjects("ns", "verifier:v1", "my-secret", labels, nil)

		assert.Equal(t, "verifier", dep.Name)
		require.Len(t, dep.Spec.Template.Spec.Containers, 1)
		c := dep.Spec.Template.Spec.Containers[0]
		assert.Equal(t, "verifier:v1", c.Image)
		require.NotNil(t, c.ReadinessProbe)
		assert.Equal(t, "/plugins", c.ReadinessProbe.HTTPGet.Path)
		require.Len(t, c.VolumeMounts, 1)
		assert.Equal(t, "/app/config.json", c.VolumeMounts[0].MountPath)
		assert.Equal(t, "config.json", c.VolumeMounts[0].SubPath)
		require.Len(t, dep.Spec.Template.Spec.ImagePullSecrets, 1)
		assert.Equal(t, "my-secret", dep.Spec.Template.Spec.ImagePullSecrets[0].Name)

		assert.Equal(t, "verifier", svc.Name)
		require.Len(t, svc.Spec.Ports, 1)
		assert.Equal(t, int32(8080), svc.Spec.Ports[0].Port)
	})

	t.Run("without pull secret", func(t *testing.T) {
		dep, _ := verifierDeploymentObjects("ns", "verifier:v1", "", labels, nil)
		assert.Empty(t, dep.Spec.Template.Spec.ImagePullSecrets)
	})
}

func TestVerifierWorkerDeployment(t *testing.T) {
	dep := verifierWorkerDeployment("ns", "worker:v1", "my-secret", nil)

	assert.Equal(t, "verifier-worker", dep.Name)
	require.Len(t, dep.Spec.Template.Spec.Containers, 1)
	c := dep.Spec.Template.Spec.Containers[0]
	assert.Equal(t, "worker:v1", c.Image)
	require.Len(t, c.VolumeMounts, 1)
	assert.Equal(t, "/app/config.json", c.VolumeMounts[0].MountPath)
}

func TestSeederJob(t *testing.T) {
	envVars := testrunnerEnvVars(testK8sCfg)
	job := seederJob("ns", "testrunner:v1", "", nil, envVars, 300, nil)

	assert.Equal(t, "seeder", job.Name)
	assert.Equal(t, "ns", job.Namespace)
	require.Len(t, job.Spec.Template.Spec.Containers, 1)
	c := job.Spec.Template.Spec.Containers[0]
	assert.Equal(t, "testrunner", c.Name)
	assert.Equal(t, "testrunner:v1", c.Image)
	assert.Equal(t, []string{"seed"}, c.Args)
	assert.NotEmpty(t, c.Env)
	assert.Equal(t, int32(0), *job.Spec.BackoffLimit)
	require.NotNil(t, job.Spec.TTLSecondsAfterFinished)
	assert.Equal(t, int32(300), *job.Spec.TTLSecondsAfterFinished)
}

func TestTestJob(t *testing.T) {
	envVars := testrunnerEnvVars(testK8sCfg)
	job := testJob("ns", "testrunner:v1", "my-secret", nil, envVars, 0, nil)

	assert.Equal(t, "test", job.Name)
	require.Len(t, job.Spec.Template.Spec.Containers, 1)
	assert.Equal(t, []string{"test"}, job.Spec.Template.Spec.Containers[0].Args)
	assert.Nil(t, job.Spec.TTLSecondsAfterFinished)
	require.Len(t, job.Spec.Template.Spec.ImagePullSecrets, 1)
}

func TestTestrunnerEnvVars(t *testing.T) {
	vars := testrunnerEnvVars(testK8sCfg)

	envMap := make(map[string]string)
	for _, v := range vars {
		envMap[v.Name] = v.Value
	}

	assert.Contains(t, envMap, "POSTGRES_DSN")
	assert.Contains(t, envMap, "MINIO_ENDPOINT")
	assert.Contains(t, envMap, "ENCRYPTION_SECRET")
	assert.Equal(t, "test-secret", envMap["ENCRYPTION_SECRET"])
	assert.Equal(t, "jwt-test-secret", envMap["JWT_SECRET"])
	assert.Equal(t, "https://plugin.example.com", envMap["PLUGIN_ENDPOINT"])
	assert.Equal(t, "http://verifier:8080", envMap["VERIFIER_URL"])
}

func TestMergeLabels(t *testing.T) {
	base := map[string]string{"a": "1", "b": "2"}
	extra := map[string]string{"c": "3", "a": "override"}

	merged := mergeLabels(base, extra)

	assert.Equal(t, "override", merged["a"])
	assert.Equal(t, "2", merged["b"])
	assert.Equal(t, "3", merged["c"])
	assert.Equal(t, "1", base["a"])
}
