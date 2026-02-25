package worker

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/vultisig/plugin-tests/config"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

type verifierJSON struct {
	Server struct {
		Host      string `json:"host"`
		Port      int    `json:"port"`
		JWTSecret string `json:"jwt_secret"`
	} `json:"server"`
	Database struct {
		DSN string `json:"dsn"`
	} `json:"database"`
	Redis struct {
		Host string `json:"host"`
		Port string `json:"port"`
	} `json:"redis"`
	BlockStorage struct {
		Host      string `json:"host"`
		Region    string `json:"region"`
		AccessKey string `json:"access_key"`
		Secret    string `json:"secret"`
		Bucket    string `json:"bucket"`
	} `json:"block_storage"`
	EncryptionSecret string `json:"encryption_secret"`
	Auth             struct {
		Enabled bool `json:"enabled"`
	} `json:"auth"`
	Fees struct {
		USDCAddress string `json:"usdc_address"`
	} `json:"fees"`
}

type workerJSON struct {
	VaultService struct {
		Relay struct {
			Server string `json:"server"`
		} `json:"relay"`
		LocalPartyPrefix string `json:"local_party_prefix"`
		EncryptionSecret string `json:"encryption_secret"`
		DoSetupMsg       bool   `json:"do_setup_msg"`
	} `json:"vault_service"`
	Database struct {
		DSN string `json:"dsn"`
	} `json:"database"`
	Redis struct {
		Host string `json:"host"`
		Port string `json:"port"`
	} `json:"redis"`
	BlockStorage struct {
		Host      string `json:"host"`
		Region    string `json:"region"`
		AccessKey string `json:"access_key"`
		Secret    string `json:"secret"`
		Bucket    string `json:"bucket"`
	} `json:"block_storage"`
	Fees struct {
		USDCAddress string `json:"usdc_address"`
	} `json:"fees"`
}

func int32Ptr(i int32) *int32 { return &i }
func boolPtr(b bool) *bool    { return &b }

func parseHostAliases(raw string) []corev1.HostAlias {
	if raw == "" {
		return nil
	}
	var aliases []corev1.HostAlias
	for _, entry := range strings.Split(raw, ",") {
		parts := strings.SplitN(strings.TrimSpace(entry), "=", 2)
		if len(parts) != 2 {
			continue
		}
		hostname := strings.TrimSpace(parts[0])
		ip := strings.TrimSpace(parts[1])
		if hostname == "" || ip == "" {
			continue
		}
		aliases = append(aliases, corev1.HostAlias{
			IP:        ip,
			Hostnames: []string{hostname},
		})
	}
	return aliases
}

func infraPostgresObjects(ns, image string, labels map[string]string) (*appsv1.Deployment, *corev1.Service) {
	selectorLabels := map[string]string{"app": "postgres"}
	allLabels := mergeLabels(labels, selectorLabels)

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "postgres", Namespace: ns, Labels: allLabels},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{MatchLabels: selectorLabels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: allLabels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:            "postgres",
						Image:           image,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Env: []corev1.EnvVar{
							{Name: "POSTGRES_USER", Value: "vultisig"},
							{Name: "POSTGRES_PASSWORD", Value: "vultisig"},
							{Name: "POSTGRES_DB", Value: "vultisig-verifier"},
						},
						Ports: []corev1.ContainerPort{{ContainerPort: 5432}},
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(5432)},
							},
							InitialDelaySeconds: 5,
							PeriodSeconds:       3,
						},
						Resources: infraResources(),
					}},
				},
			},
		},
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "postgres", Namespace: ns, Labels: allLabels},
		Spec: corev1.ServiceSpec{
			Selector: selectorLabels,
			Ports:    []corev1.ServicePort{{Port: 5432, TargetPort: intstr.FromInt32(5432)}},
		},
	}
	return dep, svc
}

func infraRedisObjects(ns, image string, labels map[string]string) (*appsv1.Deployment, *corev1.Service) {
	selectorLabels := map[string]string{"app": "redis"}
	allLabels := mergeLabels(labels, selectorLabels)

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "redis", Namespace: ns, Labels: allLabels},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{MatchLabels: selectorLabels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: allLabels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:            "redis",
						Image:           image,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Ports:           []corev1.ContainerPort{{ContainerPort: 6379}},
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(6379)},
							},
							InitialDelaySeconds: 3,
							PeriodSeconds:       3,
						},
						Resources: infraResources(),
					}},
				},
			},
		},
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "redis", Namespace: ns, Labels: allLabels},
		Spec: corev1.ServiceSpec{
			Selector: selectorLabels,
			Ports:    []corev1.ServicePort{{Port: 6379, TargetPort: intstr.FromInt32(6379)}},
		},
	}
	return dep, svc
}

func infraMinioObjects(ns, image string, labels map[string]string) (*appsv1.Deployment, *corev1.Service) {
	selectorLabels := map[string]string{"app": "minio"}
	allLabels := mergeLabels(labels, selectorLabels)

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "minio", Namespace: ns, Labels: allLabels},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{MatchLabels: selectorLabels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: allLabels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:            "minio",
						Image:           image,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Command:         []string{"minio", "server", "/data"},
						Env: []corev1.EnvVar{
							{Name: "MINIO_ROOT_USER", Value: "minioadmin"},
							{Name: "MINIO_ROOT_PASSWORD", Value: "minioadmin"},
						},
						Ports: []corev1.ContainerPort{{ContainerPort: 9000}},
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								HTTPGet: &corev1.HTTPGetAction{
									Path: "/minio/health/live",
									Port: intstr.FromInt32(9000),
								},
							},
							InitialDelaySeconds: 5,
							PeriodSeconds:       3,
						},
						Resources: infraResources(),
					}},
				},
			},
		},
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "minio", Namespace: ns, Labels: allLabels},
		Spec: corev1.ServiceSpec{
			Selector: selectorLabels,
			Ports:    []corev1.ServicePort{{Port: 9000, TargetPort: intstr.FromInt32(9000)}},
		},
	}
	return dep, svc
}

func verifierConfigMap(ns string, cfg config.K8sJobConfig) (*corev1.ConfigMap, error) {
	var v verifierJSON
	v.Server.Host = "0.0.0.0"
	v.Server.Port = 8080
	v.Server.JWTSecret = cfg.JWTSecret
	v.Database.DSN = "postgres://vultisig:vultisig@postgres:5432/vultisig-verifier?sslmode=disable"
	v.Redis.Host = "redis"
	v.Redis.Port = "6379"
	v.BlockStorage.Host = "http://minio:9000"
	v.BlockStorage.Region = "us-east-1"
	v.BlockStorage.AccessKey = "minioadmin"
	v.BlockStorage.Secret = "minioadmin"
	v.BlockStorage.Bucket = "vultisig-verifier"
	v.EncryptionSecret = cfg.EncryptionSecret
	v.Auth.Enabled = true
	v.Fees.USDCAddress = "0x0000000000000000000000000000000000000000"

	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal verifier config: %w", err)
	}

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "verifier-config", Namespace: ns},
		Data:       map[string]string{"config.json": string(data)},
	}, nil
}

func workerConfigMap(ns string, cfg config.K8sJobConfig) (*corev1.ConfigMap, error) {
	var w workerJSON
	w.VaultService.Relay.Server = "https://api.vultisig.com/router"
	w.VaultService.LocalPartyPrefix = "verifier"
	w.VaultService.EncryptionSecret = cfg.EncryptionSecret
	w.VaultService.DoSetupMsg = false
	w.Database.DSN = "postgres://vultisig:vultisig@postgres:5432/vultisig-verifier?sslmode=disable"
	w.Redis.Host = "redis"
	w.Redis.Port = "6379"
	w.BlockStorage.Host = "http://minio:9000"
	w.BlockStorage.Region = "us-east-1"
	w.BlockStorage.AccessKey = "minioadmin"
	w.BlockStorage.Secret = "minioadmin"
	w.BlockStorage.Bucket = "vultisig-verifier"
	w.Fees.USDCAddress = "0x0000000000000000000000000000000000000000"

	data, err := json.MarshalIndent(w, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal worker config: %w", err)
	}

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-config", Namespace: ns},
		Data:       map[string]string{"config.json": string(data)},
	}, nil
}

func verifierDeploymentObjects(ns, image, pullSecret string, labels map[string]string, hostAliases []corev1.HostAlias) (*appsv1.Deployment, *corev1.Service) {
	selectorLabels := map[string]string{"app": "verifier"}
	allLabels := mergeLabels(labels, selectorLabels)

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "verifier", Namespace: ns, Labels: allLabels},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{MatchLabels: selectorLabels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: allLabels},
				Spec: corev1.PodSpec{
					EnableServiceLinks: boolPtr(false),
					HostAliases:        hostAliases,
					Containers: []corev1.Container{{
						Name:            "verifier",
						Image:           image,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Ports:           []corev1.ContainerPort{{ContainerPort: 8080}},
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								HTTPGet: &corev1.HTTPGetAction{
									Path: "/plugins",
									Port: intstr.FromInt32(8080),
								},
							},
							InitialDelaySeconds: 10,
							PeriodSeconds:       5,
						},
						Resources: verifierResources(),
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "config",
							MountPath: "/app/config.json",
							SubPath:   "config.json",
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: "config",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: "verifier-config"},
							},
						},
					}},
				},
			},
		},
	}

	if pullSecret != "" {
		dep.Spec.Template.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: pullSecret}}
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "verifier", Namespace: ns, Labels: allLabels},
		Spec: corev1.ServiceSpec{
			Selector: selectorLabels,
			Ports:    []corev1.ServicePort{{Port: 8080, TargetPort: intstr.FromInt32(8080)}},
		},
	}
	return dep, svc
}

func verifierIngress(ns, host, tlsSecretName string, labels map[string]string) *networkingv1.Ingress {
	pathType := networkingv1.PathTypePrefix
	ingressClassName := "nginx"

	annotations := map[string]string{}
	if tlsSecretName == "" {
		annotations["nginx.ingress.kubernetes.io/ssl-redirect"] = "false"
	}

	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "verifier",
			Namespace:   ns,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &ingressClassName,
			Rules: []networkingv1.IngressRule{
				{
					Host: host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &pathType,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "verifier",
											Port: networkingv1.ServiceBackendPort{
												Number: 8080,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	if tlsSecretName != "" {
		ing.Spec.TLS = []networkingv1.IngressTLS{
			{
				Hosts:      []string{host},
				SecretName: tlsSecretName,
			},
		}
	}

	return ing
}

func verifierWorkerDeployment(ns, image, pullSecret string, labels map[string]string) *appsv1.Deployment {
	selectorLabels := map[string]string{"app": "verifier-worker"}
	allLabels := mergeLabels(labels, selectorLabels)

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "verifier-worker", Namespace: ns, Labels: allLabels},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{MatchLabels: selectorLabels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: allLabels},
				Spec: corev1.PodSpec{
					EnableServiceLinks: boolPtr(false),
					Containers: []corev1.Container{{
						Name:            "worker",
						Image:           image,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Resources:       verifierResources(),
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "config",
							MountPath: "/app/config.json",
							SubPath:   "config.json",
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: "config",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: "worker-config"},
							},
						},
					}},
				},
			},
		},
	}

	if pullSecret != "" {
		dep.Spec.Template.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: pullSecret}}
	}

	return dep
}

func seederJob(ns, image, pullSecret string, labels map[string]string, envVars []corev1.EnvVar, ttlSeconds int32, hostAliases []corev1.HostAlias) *batchv1.Job {
	return buildTestJob("seeder", ns, image, pullSecret, labels, []string{"seed"}, envVars, ttlSeconds, hostAliases)
}

func smokeJob(ns, image, pullSecret string, labels map[string]string, envVars []corev1.EnvVar, ttlSeconds int32, hostAliases []corev1.HostAlias) *batchv1.Job {
	return buildTestJob("smoke", ns, image, pullSecret, labels, []string{"smoke"}, envVars, ttlSeconds, hostAliases)
}

func integrationJob(ns, image, pullSecret string, labels map[string]string, envVars []corev1.EnvVar, ttlSeconds int32, hostAliases []corev1.HostAlias) *batchv1.Job {
	return buildTestJob("integration", ns, image, pullSecret, labels, []string{"integration"}, envVars, ttlSeconds, hostAliases)
}

func buildTestJob(name, ns, image, pullSecret string, labels map[string]string, args []string, envVars []corev1.EnvVar, ttlSeconds int32, hostAliases []corev1.HostAlias) *batchv1.Job {
	var backoffLimit int32
	var ttlPtr *int32
	if ttlSeconds > 0 {
		ttlPtr = &ttlSeconds
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: ttlPtr,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					HostAliases:   hostAliases,
					Containers: []corev1.Container{{
						Name:            "testrunner",
						Image:           image,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Command:         []string{"/app/main"},
						Args:            args,
						Env:             envVars,
						Resources:       jobResources(),
					}},
				},
			},
		},
	}

	if pullSecret != "" {
		job.Spec.Template.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: pullSecret}}
	}

	return job
}

func testrunnerEnvVars(cfg config.K8sJobConfig) []corev1.EnvVar {
	vars := []corev1.EnvVar{
		{Name: "POSTGRES_DSN", Value: "postgres://vultisig:vultisig@postgres:5432/vultisig-verifier?sslmode=disable"},
		{Name: "MINIO_ENDPOINT", Value: "http://minio:9000"},
		{Name: "MINIO_ACCESS_KEY", Value: "minioadmin"},
		{Name: "MINIO_SECRET_KEY", Value: "minioadmin"},
		{Name: "MINIO_BUCKET", Value: "vultisig-verifier"},
		{Name: "ENCRYPTION_SECRET", Value: cfg.EncryptionSecret},
		{Name: "VERIFIER_URL", Value: "http://verifier:8080"},
		{Name: "JWT_SECRET", Value: cfg.JWTSecret},
		{Name: "PLUGIN_ENDPOINT", Value: cfg.PluginEndpoint},
	}
	if cfg.PluginAPIKey != "" {
		vars = append(vars, corev1.EnvVar{Name: "PLUGIN_API_KEY", Value: cfg.PluginAPIKey})
	}
	if cfg.VaultB64 != "" {
		vars = append(vars, corev1.EnvVar{Name: "VAULT_B64", Value: cfg.VaultB64})
	}
	if cfg.ServerVaultB64 != "" {
		vars = append(vars, corev1.EnvVar{Name: "SERVER_VAULT_B64", Value: cfg.ServerVaultB64})
	}
	return vars
}

func integrationJobEnvVars(cfg config.K8sJobConfig, pluginID string) []corev1.EnvVar {
	vars := testrunnerEnvVars(cfg)
	vars = append(vars,
		corev1.EnvVar{Name: "RELAY_URL", Value: "https://api.vultisig.com/router"},
		corev1.EnvVar{Name: "PLUGIN_ID", Value: pluginID},
	)
	if cfg.TestTargetAddress != "" {
		vars = append(vars, corev1.EnvVar{Name: "TEST_TARGET_ADDRESS", Value: cfg.TestTargetAddress})
	}
	return vars
}

func infraResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("250m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
	}
}

func verifierResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("250m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		},
	}
}

func jobResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("250m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
	}
}

func mergeLabels(base, extra map[string]string) map[string]string {
	merged := make(map[string]string, len(base)+len(extra))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range extra {
		merged[k] = v
	}
	return merged
}
