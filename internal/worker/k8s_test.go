package worker

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCreateNamespace(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()
		labels := map[string]string{"app": "test"}

		err := createNamespace(context.Background(), clientset, "test-ns", labels)
		require.NoError(t, err)

		ns, err := clientset.CoreV1().Namespaces().Get(context.Background(), "test-ns", metav1.GetOptions{})
		require.NoError(t, err)
		assert.Equal(t, "test-ns", ns.Name)
	})

	t.Run("already exists", func(t *testing.T) {
		clientset := fake.NewSimpleClientset(&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
		})

		err := createNamespace(context.Background(), clientset, "test-ns", nil)
		assert.NoError(t, err)
	})

	t.Run("labels applied", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()
		labels := map[string]string{
			labelManagedBy: managedByValue,
			labelRunID:     "run-123",
		}

		err := createNamespace(context.Background(), clientset, "labeled-ns", labels)
		require.NoError(t, err)

		ns, err := clientset.CoreV1().Namespaces().Get(context.Background(), "labeled-ns", metav1.GetOptions{})
		require.NoError(t, err)
		assert.Equal(t, managedByValue, ns.Labels[labelManagedBy])
		assert.Equal(t, "run-123", ns.Labels[labelRunID])
	})
}

func TestDeleteNamespace(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		clientset := fake.NewSimpleClientset(&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "del-ns"},
		})

		err := deleteNamespace(context.Background(), clientset, "del-ns")
		require.NoError(t, err)

		_, err = clientset.CoreV1().Namespaces().Get(context.Background(), "del-ns", metav1.GetOptions{})
		assert.True(t, k8serrors.IsNotFound(err))
	})

	t.Run("not found is not an error", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()

		err := deleteNamespace(context.Background(), clientset, "nonexistent")
		assert.NoError(t, err)
	})
}

func TestCreateDenyAllNetworkPolicy(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()

		err := createDenyAllNetworkPolicy(context.Background(), clientset, "test-ns")
		require.NoError(t, err)

		policy, err := clientset.NetworkingV1().NetworkPolicies("test-ns").Get(context.Background(), "deny-all", metav1.GetOptions{})
		require.NoError(t, err)
		assert.Equal(t, "deny-all", policy.Name)
		assert.Len(t, policy.Spec.PolicyTypes, 2)
		require.Len(t, policy.Spec.Egress, 1)
		assert.Len(t, policy.Spec.Egress[0].Ports, 2)
	})

	t.Run("already exists", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()

		err := createDenyAllNetworkPolicy(context.Background(), clientset, "test-ns")
		require.NoError(t, err)

		err = createDenyAllNetworkPolicy(context.Background(), clientset, "test-ns")
		assert.NoError(t, err)
	})
}

func TestCreateDummyJob(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()
		labels := map[string]string{"app": "test"}

		job, err := createDummyJob(context.Background(), clientset, "ns", "test-job", "run-123", labels, 300)
		require.NoError(t, err)
		require.NotNil(t, job)

		assert.Equal(t, "test-job", job.Name)
		assert.Equal(t, "ns", job.Namespace)
		assert.Equal(t, labels, job.Labels)

		containers := job.Spec.Template.Spec.Containers
		require.Len(t, containers, 1)
		assert.Equal(t, defaultDummyImage, containers[0].Image)
		assert.Equal(t, int32(0), *job.Spec.BackoffLimit)
		assert.Equal(t, corev1.RestartPolicyNever, job.Spec.Template.Spec.RestartPolicy)
		assert.Contains(t, containers[0].Command[2], "$RUN_ID")

		require.Len(t, containers[0].Env, 1)
		assert.Equal(t, "RUN_ID", containers[0].Env[0].Name)
		assert.Equal(t, "run-123", containers[0].Env[0].Value)

		assert.NotNil(t, containers[0].Resources.Limits)
		assert.NotNil(t, containers[0].Resources.Requests)
	})

	t.Run("ttl set when positive", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()

		job, err := createDummyJob(context.Background(), clientset, "ns", "job-ttl", "run-1", nil, 300)
		require.NoError(t, err)
		require.NotNil(t, job.Spec.TTLSecondsAfterFinished)
		assert.Equal(t, int32(300), *job.Spec.TTLSecondsAfterFinished)
	})

	t.Run("ttl nil when zero", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()

		job, err := createDummyJob(context.Background(), clientset, "ns", "job-nottl", "run-2", nil, 0)
		require.NoError(t, err)
		assert.Nil(t, job.Spec.TTLSecondsAfterFinished)
	})

	t.Run("already exists returns existing", func(t *testing.T) {
		existing := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "existing-job",
				Namespace: "ns",
			},
		}
		clientset := fake.NewSimpleClientset(existing)

		job, err := createDummyJob(context.Background(), clientset, "ns", "existing-job", "run-3", nil, 0)
		require.NoError(t, err)
		require.NotNil(t, job)
		assert.Equal(t, "existing-job", job.Name)
	})
}

func TestWaitForJob(t *testing.T) {
	t.Run("succeeded", func(t *testing.T) {
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: "ok-job", Namespace: "ns"},
			Status:     batchv1.JobStatus{Succeeded: 1},
		}
		clientset := fake.NewSimpleClientset(job)

		passed, err := waitForJob(context.Background(), clientset, "ns", "ok-job", 5*time.Second, 50*time.Millisecond)
		require.NoError(t, err)
		assert.True(t, passed)
	})

	t.Run("failed", func(t *testing.T) {
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: "fail-job", Namespace: "ns"},
			Status:     batchv1.JobStatus{Failed: 1},
		}
		clientset := fake.NewSimpleClientset(job)

		passed, err := waitForJob(context.Background(), clientset, "ns", "fail-job", 5*time.Second, 50*time.Millisecond)
		require.NoError(t, err)
		assert.False(t, passed)
	})

	t.Run("timeout", func(t *testing.T) {
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: "slow-job", Namespace: "ns"},
			Status:     batchv1.JobStatus{},
		}
		clientset := fake.NewSimpleClientset(job)

		_, err := waitForJob(context.Background(), clientset, "ns", "slow-job", 150*time.Millisecond, 50*time.Millisecond)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "timeout")
	})

	t.Run("context cancelled", func(t *testing.T) {
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: "cancel-job", Namespace: "ns"},
		}
		clientset := fake.NewSimpleClientset(job)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := waitForJob(ctx, clientset, "ns", "cancel-job", 5*time.Second, 50*time.Millisecond)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestFetchJobLogs(t *testing.T) {
	t.Run("no pods found", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()

		_, err := fetchJobLogs(context.Background(), clientset, "ns", "no-pods-job", 1, 10*time.Millisecond)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no pods found")
	})

	t.Run("retry exhaustion", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()

		_, err := fetchJobLogs(context.Background(), clientset, "ns", "no-pods-job", 3, 10*time.Millisecond)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "3 attempts")
	})
}

func TestCreateIntraNamespaceNetworkPolicy(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()

		err := createIntraNamespaceNetworkPolicy(context.Background(), clientset, "test-ns")
		require.NoError(t, err)

		policy, err := clientset.NetworkingV1().NetworkPolicies("test-ns").Get(context.Background(), "allow-intra-namespace", metav1.GetOptions{})
		require.NoError(t, err)
		assert.Equal(t, "allow-intra-namespace", policy.Name)
		assert.Len(t, policy.Spec.PolicyTypes, 2)
		require.Len(t, policy.Spec.Ingress, 1)
		require.Len(t, policy.Spec.Egress, 3)
	})

	t.Run("already exists", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()

		err := createIntraNamespaceNetworkPolicy(context.Background(), clientset, "test-ns")
		require.NoError(t, err)

		err = createIntraNamespaceNetworkPolicy(context.Background(), clientset, "test-ns")
		assert.NoError(t, err)
	})
}

func TestApplyDeployment(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()
		dep := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "test-dep", Namespace: "ns"},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "test"}},
					Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "img"}}},
				},
			},
		}

		err := applyDeployment(context.Background(), clientset, dep)
		require.NoError(t, err)

		got, err := clientset.AppsV1().Deployments("ns").Get(context.Background(), "test-dep", metav1.GetOptions{})
		require.NoError(t, err)
		assert.Equal(t, "test-dep", got.Name)
	})

	t.Run("already exists", func(t *testing.T) {
		dep := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "test-dep", Namespace: "ns"},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "test"}},
					Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "img"}}},
				},
			},
		}
		clientset := fake.NewSimpleClientset(dep)

		err := applyDeployment(context.Background(), clientset, dep)
		assert.NoError(t, err)
	})
}

func TestApplyService(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "test-svc", Namespace: "ns"},
			Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80}}},
		}

		err := applyService(context.Background(), clientset, svc)
		require.NoError(t, err)

		got, err := clientset.CoreV1().Services("ns").Get(context.Background(), "test-svc", metav1.GetOptions{})
		require.NoError(t, err)
		assert.Equal(t, "test-svc", got.Name)
	})
}

func TestApplyConfigMap(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: "ns"},
			Data:       map[string]string{"key": "val"},
		}

		err := applyConfigMap(context.Background(), clientset, cm)
		require.NoError(t, err)

		got, err := clientset.CoreV1().ConfigMaps("ns").Get(context.Background(), "test-cm", metav1.GetOptions{})
		require.NoError(t, err)
		assert.Equal(t, "val", got.Data["key"])
	})
}

func TestApplyJob(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: "test-job", Namespace: "ns"},
		}

		created, err := applyJob(context.Background(), clientset, job)
		require.NoError(t, err)
		assert.Equal(t, "test-job", created.Name)
	})

	t.Run("already exists returns existing", func(t *testing.T) {
		existing := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: "test-job", Namespace: "ns"},
		}
		clientset := fake.NewSimpleClientset(existing)

		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: "test-job", Namespace: "ns"},
		}
		got, err := applyJob(context.Background(), clientset, job)
		require.NoError(t, err)
		assert.Equal(t, "test-job", got.Name)
	})
}

func TestWaitForDeploymentReady(t *testing.T) {
	t.Run("ready immediately", func(t *testing.T) {
		dep := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "ready-dep", Namespace: "ns"},
			Status:     appsv1.DeploymentStatus{ReadyReplicas: 1},
		}
		clientset := fake.NewSimpleClientset(dep)

		err := waitForDeploymentReady(context.Background(), clientset, "ns", "ready-dep", 5*time.Second, 50*time.Millisecond)
		assert.NoError(t, err)
	})

	t.Run("timeout", func(t *testing.T) {
		dep := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "slow-dep", Namespace: "ns"},
			Status:     appsv1.DeploymentStatus{ReadyReplicas: 0},
		}
		clientset := fake.NewSimpleClientset(dep)

		err := waitForDeploymentReady(context.Background(), clientset, "ns", "slow-dep", 150*time.Millisecond, 50*time.Millisecond)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "timeout")
	})

	t.Run("context cancelled", func(t *testing.T) {
		dep := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "cancel-dep", Namespace: "ns"},
		}
		clientset := fake.NewSimpleClientset(dep)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := waitForDeploymentReady(ctx, clientset, "ns", "cancel-dep", 5*time.Second, 50*time.Millisecond)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestFetchJobLogsByContainer(t *testing.T) {
	t.Run("no pods found", func(t *testing.T) {
		clientset := fake.NewSimpleClientset()

		_, err := fetchJobLogsByContainer(context.Background(), clientset, "ns", "no-pods-job", "testrunner", 1, 10*time.Millisecond)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no pods found")
	})
}
