package worker

import (
	"context"
	"fmt"
	"io"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
)

const (
	defaultDummyImage = "busybox:1.36"
	maxLogBytes       = 1 << 20 // 1MB
)

func createNamespace(ctx context.Context, clientset kubernetes.Interface, name string, labels map[string]string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
	_, err := clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		if k8serrors.IsAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("failed to create namespace %s: %w", name, err)
	}
	return nil
}

func createDenyAllNetworkPolicy(ctx context.Context, clientset kubernetes.Interface, namespace string) error {
	dnsPort := intstr.FromInt32(53)
	udp := corev1.ProtocolUDP
	tcp := corev1.ProtocolTCP

	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deny-all",
			Namespace: namespace,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &udp, Port: &dnsPort},
						{Protocol: &tcp, Port: &dnsPort},
					},
				},
			},
		},
	}
	_, err := clientset.NetworkingV1().NetworkPolicies(namespace).Create(ctx, policy, metav1.CreateOptions{})
	if err != nil {
		if k8serrors.IsAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("failed to create network policy in %s: %w", namespace, err)
	}
	return nil
}

func deleteNamespace(ctx context.Context, clientset kubernetes.Interface, name string) error {
	propagation := metav1.DeletePropagationForeground
	err := clientset.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{
		PropagationPolicy: &propagation,
	})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to delete namespace %s: %w", name, err)
	}
	return nil
}

func createDummyJob(ctx context.Context, clientset kubernetes.Interface, namespace string, name string, runID string, labels map[string]string, ttlSeconds int32) (*batchv1.Job, error) {
	var backoffLimit int32
	var ttlPtr *int32
	if ttlSeconds > 0 {
		ttlPtr = &ttlSeconds
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: ttlPtr,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "dummy",
							Image:   defaultDummyImage,
							Command: []string{"sh", "-c", "echo \"integration test dummy for run $RUN_ID\" && sleep 1"},
							Env: []corev1.EnvVar{
								{Name: "RUN_ID", Value: runID},
							},
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("64Mi"),
								},
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("50m"),
									corev1.ResourceMemory: resource.MustParse("32Mi"),
								},
							},
						},
					},
				},
			},
		},
	}

	created, err := clientset.BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		if k8serrors.IsAlreadyExists(err) {
			existing, err := clientset.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return nil, fmt.Errorf("job already exists but failed to get it %s: %w", name, err)
			}
			return existing, nil
		}
		return nil, fmt.Errorf("failed to create job %s: %w", name, err)
	}
	return created, nil
}

func waitForJob(ctx context.Context, clientset kubernetes.Interface, namespace string, name string, timeout time.Duration, poll time.Duration) (bool, error) {
	if poll <= 0 {
		poll = 2 * time.Second
	}
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	deadline := time.After(timeout)
	ticker := time.NewTicker(poll)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-deadline:
			return false, fmt.Errorf("timeout waiting for job %s/%s after %v", namespace, name, timeout)
		case <-ticker.C:
			job, err := clientset.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				if k8serrors.IsNotFound(err) {
					continue
				}
				return false, fmt.Errorf("failed to get job %s/%s: %w", namespace, name, err)
			}
			if job.Status.Succeeded > 0 {
				return true, nil
			}
			if job.Status.Failed > 0 {
				return false, nil
			}
		}
	}
}

func fetchJobLogs(ctx context.Context, clientset kubernetes.Interface, namespace string, name string, maxRetries int, retryDelay time.Duration) (string, error) {
	return fetchJobLogsByContainer(ctx, clientset, namespace, name, "dummy", maxRetries, retryDelay)
}

func createIntraNamespaceNetworkPolicy(ctx context.Context, clientset kubernetes.Interface, namespace string, pluginPort int32) error {
	dnsPort := intstr.FromInt32(53)
	httpsPort := intstr.FromInt32(443)
	udp := corev1.ProtocolUDP
	tcp := corev1.ProtocolTCP

	egressRules := []networkingv1.NetworkPolicyEgressRule{
		{
			To: []networkingv1.NetworkPolicyPeer{
				{PodSelector: &metav1.LabelSelector{}},
			},
		},
		{
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &udp, Port: &dnsPort},
				{Protocol: &tcp, Port: &dnsPort},
			},
		},
		{
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &tcp, Port: &httpsPort},
			},
		},
	}

	if pluginPort > 0 && pluginPort != 443 {
		pp := intstr.FromInt32(pluginPort)
		egressRules = append(egressRules, networkingv1.NetworkPolicyEgressRule{
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &tcp, Port: &pp},
			},
		})
	}

	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "allow-intra-namespace",
			Namespace: namespace,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: []networkingv1.NetworkPolicyPeer{
						{PodSelector: &metav1.LabelSelector{}},
					},
				},
			},
			Egress: egressRules,
		},
	}
	_, err := clientset.NetworkingV1().NetworkPolicies(namespace).Create(ctx, policy, metav1.CreateOptions{})
	if err != nil {
		if k8serrors.IsAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("failed to create network policy in %s: %w", namespace, err)
	}
	return nil
}

func applyDeployment(ctx context.Context, clientset kubernetes.Interface, dep *appsv1.Deployment) error {
	_, err := clientset.AppsV1().Deployments(dep.Namespace).Create(ctx, dep, metav1.CreateOptions{})
	if err != nil {
		if k8serrors.IsAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("failed to create deployment %s: %w", dep.Name, err)
	}
	return nil
}

func applyService(ctx context.Context, clientset kubernetes.Interface, svc *corev1.Service) error {
	_, err := clientset.CoreV1().Services(svc.Namespace).Create(ctx, svc, metav1.CreateOptions{})
	if err != nil {
		if k8serrors.IsAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("failed to create service %s: %w", svc.Name, err)
	}
	return nil
}

func applyConfigMap(ctx context.Context, clientset kubernetes.Interface, cm *corev1.ConfigMap) error {
	_, err := clientset.CoreV1().ConfigMaps(cm.Namespace).Create(ctx, cm, metav1.CreateOptions{})
	if err != nil {
		if k8serrors.IsAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("failed to create configmap %s: %w", cm.Name, err)
	}
	return nil
}

func applyJob(ctx context.Context, clientset kubernetes.Interface, job *batchv1.Job) (*batchv1.Job, error) {
	created, err := clientset.BatchV1().Jobs(job.Namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		if k8serrors.IsAlreadyExists(err) {
			existing, getErr := clientset.BatchV1().Jobs(job.Namespace).Get(ctx, job.Name, metav1.GetOptions{})
			if getErr != nil {
				return nil, fmt.Errorf("job already exists but failed to get %s: %w", job.Name, getErr)
			}
			return existing, nil
		}
		return nil, fmt.Errorf("failed to create job %s: %w", job.Name, err)
	}
	return created, nil
}

func waitForDeploymentReady(ctx context.Context, clientset kubernetes.Interface, namespace, name string, timeout, poll time.Duration) error {
	if poll <= 0 {
		poll = 2 * time.Second
	}
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	deadline := time.After(timeout)
	ticker := time.NewTicker(poll)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("timeout waiting for deployment %s/%s after %v", namespace, name, timeout)
		case <-ticker.C:
			dep, err := clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				if k8serrors.IsNotFound(err) {
					continue
				}
				return fmt.Errorf("failed to get deployment %s/%s: %w", namespace, name, err)
			}
			if dep.Status.ReadyReplicas >= 1 {
				return nil
			}
		}
	}
}

func fetchJobLogsByContainer(ctx context.Context, clientset kubernetes.Interface, namespace, jobName, containerName string, maxRetries int, retryDelay time.Duration) (string, error) {
	if maxRetries <= 0 {
		maxRetries = 1
	}
	if retryDelay <= 0 {
		retryDelay = 1 * time.Second
	}

	var pods *corev1.PodList
	var err error

	for attempt := 0; attempt < maxRetries; attempt++ {
		pods, err = clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("job-name=%s", jobName),
		})
		if err != nil {
			return "", fmt.Errorf("failed to list pods for job %s: %w", jobName, err)
		}
		if len(pods.Items) > 0 {
			break
		}
		if attempt < maxRetries-1 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(retryDelay):
			}
		}
	}

	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no pods found for job %s after %d attempts", jobName, maxRetries)
	}

	newest := pods.Items[0]
	for _, pod := range pods.Items[1:] {
		if pod.CreationTimestamp.After(newest.CreationTimestamp.Time) {
			newest = pod
		}
	}

	stream, err := clientset.CoreV1().Pods(namespace).GetLogs(newest.Name, &corev1.PodLogOptions{
		Container: containerName,
	}).Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get logs for pod %s container %s: %w", newest.Name, containerName, err)
	}
	defer stream.Close()

	buf, err := io.ReadAll(io.LimitReader(stream, maxLogBytes))
	if err != nil {
		return "", fmt.Errorf("failed to read logs: %w", err)
	}
	return string(buf), nil
}
