package worker

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"

	"github.com/vultisig/plugin-tests/config"
)

const (
	defaultPostgresImage = "postgres:15"
	defaultRedisImage    = "redis:7"
	defaultMinioImage    = "minio/minio:latest"
)

type Runner struct {
	k8s    kubernetes.Interface
	cfg    config.K8sJobConfig
	logger *logrus.Entry
}

type RunResult struct {
	Passed     bool
	SeederLogs string
	TestLogs   string
	ErrorMsg   string
}

func NewRunner(k8s kubernetes.Interface, cfg config.K8sJobConfig, logger *logrus.Entry) *Runner {
	return &Runner{
		k8s:    k8s,
		cfg:    cfg,
		logger: logger,
	}
}

func (r *Runner) Run(ctx context.Context, namespace, runID, pluginID string, labels map[string]string) *RunResult {
	result := &RunResult{}

	err := r.deployNetworkPolicy(ctx, namespace)
	if err != nil {
		result.ErrorMsg = err.Error()
		return result
	}

	err = r.deployInfra(ctx, namespace, labels)
	if err != nil {
		result.ErrorMsg = err.Error()
		return result
	}

	err = r.waitForInfra(ctx, namespace)
	if err != nil {
		result.ErrorMsg = err.Error()
		return result
	}

	err = r.deployConfigMaps(ctx, namespace)
	if err != nil {
		result.ErrorMsg = err.Error()
		return result
	}

	err = r.deployVerifier(ctx, namespace, labels)
	if err != nil {
		result.ErrorMsg = err.Error()
		return result
	}

	err = r.waitForVerifier(ctx, namespace)
	if err != nil {
		result.ErrorMsg = err.Error()
		return result
	}

	result.SeederLogs, err = r.runSeederJob(ctx, namespace, runID, labels)
	if err != nil {
		result.ErrorMsg = fmt.Sprintf("seeder failed: %s", err.Error())
		return result
	}

	testLogs, testPassed, err := r.runTestJob(ctx, namespace, runID, labels)
	result.TestLogs = testLogs
	if err != nil {
		result.ErrorMsg = fmt.Sprintf("test job failed: %s", err.Error())
		return result
	}

	result.Passed = testPassed
	return result
}

func (r *Runner) deployNetworkPolicy(ctx context.Context, ns string) error {
	r.logger.Info("creating network policy")
	pluginPort := parsePort(r.cfg.PluginEndpoint)
	return createIntraNamespaceNetworkPolicy(ctx, r.k8s, ns, pluginPort)
}

func parsePort(rawURL string) int32 {
	if rawURL == "" {
		return 0
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return 0
	}
	if u.Port() != "" {
		p, err := strconv.Atoi(u.Port())
		if err != nil {
			return 0
		}
		return int32(p)
	}
	if u.Scheme == "https" {
		return 443
	}
	return 80
}

func (r *Runner) deployInfra(ctx context.Context, ns string, labels map[string]string) error {
	pgImage := r.imageOrDefault(r.cfg.PostgresImage, defaultPostgresImage)
	redisImage := r.imageOrDefault(r.cfg.RedisImage, defaultRedisImage)
	minioImage := r.imageOrDefault(r.cfg.MinioImage, defaultMinioImage)

	pgDep, pgSvc := infraPostgresObjects(ns, pgImage, labels)
	err := applyDeployment(ctx, r.k8s, pgDep)
	if err != nil {
		return fmt.Errorf("deploy postgres: %w", err)
	}
	err = applyService(ctx, r.k8s, pgSvc)
	if err != nil {
		return fmt.Errorf("deploy postgres service: %w", err)
	}
	r.logger.Info("postgres deployed")

	redisDep, redisSvc := infraRedisObjects(ns, redisImage, labels)
	err = applyDeployment(ctx, r.k8s, redisDep)
	if err != nil {
		return fmt.Errorf("deploy redis: %w", err)
	}
	err = applyService(ctx, r.k8s, redisSvc)
	if err != nil {
		return fmt.Errorf("deploy redis service: %w", err)
	}
	r.logger.Info("redis deployed")

	minioDep, minioSvc := infraMinioObjects(ns, minioImage, labels)
	err = applyDeployment(ctx, r.k8s, minioDep)
	if err != nil {
		return fmt.Errorf("deploy minio: %w", err)
	}
	err = applyService(ctx, r.k8s, minioSvc)
	if err != nil {
		return fmt.Errorf("deploy minio service: %w", err)
	}
	r.logger.Info("minio deployed")

	return nil
}

func (r *Runner) waitForInfra(ctx context.Context, ns string) error {
	for _, name := range []string{"postgres", "redis", "minio"} {
		r.logger.WithField("deployment", name).Info("waiting for readiness")
		err := waitForDeploymentReady(ctx, r.k8s, ns, name, r.timeout(5*time.Minute), r.pollInterval())
		if err != nil {
			return fmt.Errorf("wait for %s: %w", name, err)
		}
	}
	r.logger.Info("infra ready")
	return nil
}

func (r *Runner) deployConfigMaps(ctx context.Context, ns string) error {
	vcm, err := verifierConfigMap(ns, r.cfg)
	if err != nil {
		return fmt.Errorf("build verifier configmap: %w", err)
	}
	err = applyConfigMap(ctx, r.k8s, vcm)
	if err != nil {
		return fmt.Errorf("apply verifier configmap: %w", err)
	}

	wcm, err := workerConfigMap(ns, r.cfg)
	if err != nil {
		return fmt.Errorf("build worker configmap: %w", err)
	}
	err = applyConfigMap(ctx, r.k8s, wcm)
	if err != nil {
		return fmt.Errorf("apply worker configmap: %w", err)
	}
	r.logger.Info("configmaps created")
	return nil
}

func (r *Runner) deployVerifier(ctx context.Context, ns string, labels map[string]string) error {
	hostAliases := parseHostAliases(r.cfg.HostAliases)
	vDep, vSvc := verifierDeploymentObjects(ns, r.cfg.VerifierImage, r.cfg.ImagePullSecret, labels, hostAliases)
	err := applyDeployment(ctx, r.k8s, vDep)
	if err != nil {
		return fmt.Errorf("deploy verifier: %w", err)
	}
	err = applyService(ctx, r.k8s, vSvc)
	if err != nil {
		return fmt.Errorf("deploy verifier service: %w", err)
	}
	r.logger.Info("verifier deployed")

	wDep := verifierWorkerDeployment(ns, r.cfg.VerifierWorkerImage, r.cfg.ImagePullSecret, labels)
	err = applyDeployment(ctx, r.k8s, wDep)
	if err != nil {
		return fmt.Errorf("deploy verifier-worker: %w", err)
	}
	r.logger.Info("verifier-worker deployed")

	return nil
}

func (r *Runner) waitForVerifier(ctx context.Context, ns string) error {
	r.logger.Info("waiting for verifier readiness")
	err := waitForDeploymentReady(ctx, r.k8s, ns, "verifier", r.timeout(5*time.Minute), r.pollInterval())
	if err != nil {
		return fmt.Errorf("wait for verifier: %w", err)
	}
	r.logger.Info("verifier ready")
	return nil
}

func (r *Runner) runSeederJob(ctx context.Context, ns, runID string, labels map[string]string) (string, error) {
	envVars := testrunnerEnvVars(r.cfg)
	name := seederJobName(runID)
	hostAliases := parseHostAliases(r.cfg.HostAliases)
	job := seederJob(ns, r.cfg.TestImage, r.cfg.ImagePullSecret, labels, envVars, r.cfg.TTLAfterFinished, hostAliases)
	job.Name = name

	r.logger.WithField("job", name).Info("running seeder")
	_, err := applyJob(ctx, r.k8s, job)
	if err != nil {
		return "", fmt.Errorf("create seeder job: %w", err)
	}

	passed, err := waitForJob(ctx, r.k8s, ns, name, r.timeout(5*time.Minute), r.pollInterval())
	if err != nil {
		return "", fmt.Errorf("wait for seeder: %w", err)
	}

	logs, logErr := fetchJobLogsByContainer(ctx, r.k8s, ns, name, "testrunner", 3, 2*time.Second)
	if logErr != nil {
		r.logger.WithError(logErr).Warn("failed to fetch seeder logs")
	}

	if !passed {
		return logs, fmt.Errorf("seeder job failed")
	}
	r.logger.Info("seeder completed")
	return logs, nil
}

func (r *Runner) runTestJob(ctx context.Context, ns, runID string, labels map[string]string) (string, bool, error) {
	envVars := testrunnerEnvVars(r.cfg)
	name := testJobName(runID)
	hostAliases := parseHostAliases(r.cfg.HostAliases)
	job := testJob(ns, r.cfg.TestImage, r.cfg.ImagePullSecret, labels, envVars, r.cfg.TTLAfterFinished, hostAliases)
	job.Name = name

	r.logger.WithField("job", name).Info("running tests")
	_, err := applyJob(ctx, r.k8s, job)
	if err != nil {
		return "", false, fmt.Errorf("create test job: %w", err)
	}

	passed, err := waitForJob(ctx, r.k8s, ns, name, r.timeout(10*time.Minute), r.pollInterval())
	if err != nil {
		return "", false, fmt.Errorf("wait for test: %w", err)
	}

	logs, logErr := fetchJobLogsByContainer(ctx, r.k8s, ns, name, "testrunner", 3, 2*time.Second)
	if logErr != nil {
		r.logger.WithError(logErr).Warn("failed to fetch test logs")
	}

	return logs, passed, nil
}

func (r *Runner) imageOrDefault(image, defaultImage string) string {
	if image != "" {
		return image
	}
	return defaultImage
}

func (r *Runner) timeout(fallback time.Duration) time.Duration {
	if r.cfg.JobTimeout > 0 {
		return r.cfg.JobTimeout
	}
	return fallback
}

func (r *Runner) pollInterval() time.Duration {
	if r.cfg.PollInterval > 0 {
		return r.cfg.PollInterval
	}
	return 2 * time.Second
}
