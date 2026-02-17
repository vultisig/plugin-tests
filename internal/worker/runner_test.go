package worker

import (
	"context"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/vultisig/plugin-tests/config"
)

func newTestRunner(clientset *fake.Clientset) *Runner {
	cfg := config.K8sJobConfig{
		JobTimeout:          5 * time.Second,
		PollInterval:        50 * time.Millisecond,
		TTLAfterFinished:    300,
		VerifierImage:       "verifier:test",
		VerifierWorkerImage: "worker:test",
		TestImage:           "testrunner:test",
		EncryptionSecret:    "test-secret",
		JWTSecret:           "jwt-secret",
		PluginEndpoint:      "https://plugin.example.com",
	}
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	entry := logger.WithField("test", true)
	return NewRunner(clientset, cfg, entry)
}

func TestRunner_DeployNetworkPolicy(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	runner := newTestRunner(clientset)

	err := runner.deployNetworkPolicy(context.Background(), "test-ns")
	require.NoError(t, err)

	policy, err := clientset.NetworkingV1().NetworkPolicies("test-ns").Get(context.Background(), "allow-intra-namespace", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "allow-intra-namespace", policy.Name)
}

func TestRunner_DeployInfra(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	runner := newTestRunner(clientset)
	labels := map[string]string{"test": "true"}

	err := runner.deployInfra(context.Background(), "test-ns", labels)
	require.NoError(t, err)

	for _, name := range []string{"postgres", "redis", "minio"} {
		dep, err := clientset.AppsV1().Deployments("test-ns").Get(context.Background(), name, metav1.GetOptions{})
		require.NoError(t, err)
		assert.Equal(t, name, dep.Name)

		svc, err := clientset.CoreV1().Services("test-ns").Get(context.Background(), name, metav1.GetOptions{})
		require.NoError(t, err)
		assert.Equal(t, name, svc.Name)
	}
}

func TestRunner_DeployInfra_CustomImages(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	runner := newTestRunner(clientset)
	runner.cfg.PostgresImage = "postgres:16"
	runner.cfg.RedisImage = "redis:8"
	runner.cfg.MinioImage = "minio/minio:2024"

	err := runner.deployInfra(context.Background(), "test-ns", nil)
	require.NoError(t, err)

	pgDep, err := clientset.AppsV1().Deployments("test-ns").Get(context.Background(), "postgres", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "postgres:16", pgDep.Spec.Template.Spec.Containers[0].Image)

	redisDep, err := clientset.AppsV1().Deployments("test-ns").Get(context.Background(), "redis", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "redis:8", redisDep.Spec.Template.Spec.Containers[0].Image)

	minioDep, err := clientset.AppsV1().Deployments("test-ns").Get(context.Background(), "minio", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "minio/minio:2024", minioDep.Spec.Template.Spec.Containers[0].Image)
}

func TestRunner_WaitForInfra_AllReady(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "postgres", Namespace: "ns"},
			Status:     appsv1.DeploymentStatus{ReadyReplicas: 1},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "redis", Namespace: "ns"},
			Status:     appsv1.DeploymentStatus{ReadyReplicas: 1},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "minio", Namespace: "ns"},
			Status:     appsv1.DeploymentStatus{ReadyReplicas: 1},
		},
	)
	runner := newTestRunner(clientset)

	err := runner.waitForInfra(context.Background(), "ns")
	assert.NoError(t, err)
}

func TestRunner_WaitForInfra_Timeout(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "postgres", Namespace: "ns"},
			Status:     appsv1.DeploymentStatus{ReadyReplicas: 1},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "redis", Namespace: "ns"},
			Status:     appsv1.DeploymentStatus{ReadyReplicas: 0},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "minio", Namespace: "ns"},
			Status:     appsv1.DeploymentStatus{ReadyReplicas: 1},
		},
	)
	runner := newTestRunner(clientset)
	runner.cfg.JobTimeout = 200 * time.Millisecond

	err := runner.waitForInfra(context.Background(), "ns")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redis")
}

func TestRunner_DeployConfigMaps(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	runner := newTestRunner(clientset)

	err := runner.deployConfigMaps(context.Background(), "test-ns")
	require.NoError(t, err)

	vcm, err := clientset.CoreV1().ConfigMaps("test-ns").Get(context.Background(), "verifier-config", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Contains(t, vcm.Data["config.json"], "jwt-secret")

	wcm, err := clientset.CoreV1().ConfigMaps("test-ns").Get(context.Background(), "worker-config", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Contains(t, wcm.Data["config.json"], "vault_service")
}

func TestRunner_DeployVerifier(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	runner := newTestRunner(clientset)

	err := runner.deployVerifier(context.Background(), "test-ns", nil)
	require.NoError(t, err)

	vDep, err := clientset.AppsV1().Deployments("test-ns").Get(context.Background(), "verifier", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "verifier:test", vDep.Spec.Template.Spec.Containers[0].Image)

	wDep, err := clientset.AppsV1().Deployments("test-ns").Get(context.Background(), "verifier-worker", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "worker:test", wDep.Spec.Template.Spec.Containers[0].Image)

	svc, err := clientset.CoreV1().Services("test-ns").Get(context.Background(), "verifier", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "verifier", svc.Name)
}

func TestRunner_ImageOrDefault(t *testing.T) {
	runner := &Runner{}

	assert.Equal(t, "custom:v1", runner.imageOrDefault("custom:v1", "default:v1"))
	assert.Equal(t, "default:v1", runner.imageOrDefault("", "default:v1"))
}
