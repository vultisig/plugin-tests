package main

import (
	"context"
	"os"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/vultisig/plugin-tests/internal/testrunner"
)

var logger = logrus.New()

func main() {
	logger.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})

	if len(os.Args) < 2 {
		logger.Fatal("usage: testrunner <seed|test|install>")
	}

	switch os.Args[1] {
	case "seed":
		runSeed()
	case "test":
		runTest()
	case "install":
		runInstall()
	default:
		logger.Fatalf("unknown command: %s", os.Args[1])
	}
}

func runSeed() {
	ctx := context.Background()

	fixture, err := testrunner.LoadFixture()
	if err != nil {
		logger.WithError(err).Fatal("failed to load fixture")
	}

	plugins := testrunner.GetTestPlugins()

	seeder := testrunner.NewSeeder(testrunner.SeederConfig{
		DSN: requireEnv("POSTGRES_DSN"),
		S3: testrunner.S3Config{
			Endpoint:  requireEnv("MINIO_ENDPOINT"),
			Region:    "us-east-1",
			AccessKey: requireEnv("MINIO_ACCESS_KEY"),
			SecretKey: requireEnv("MINIO_SECRET_KEY"),
			Bucket:    requireEnv("MINIO_BUCKET"),
		},
		Fixture:          fixture,
		Plugins:          plugins,
		EncryptionSecret: requireEnv("ENCRYPTION_SECRET"),
	}, logger)

	logger.Info("seeding database")
	err = seeder.SeedDatabase(ctx)
	if err != nil {
		logger.WithError(err).Fatal("failed to seed database")
	}

	logger.Info("seeding vaults to MinIO")
	err = seeder.SeedVaults(ctx)
	if err != nil {
		logger.WithError(err).Fatal("failed to seed vaults")
	}

	logger.Info("seeding completed successfully")
}

func runTest() {
	fixture, err := testrunner.LoadFixture()
	if err != nil {
		logger.WithError(err).Fatal("failed to load fixture")
	}

	plugins := testrunner.GetTestPlugins()
	verifierURL := requireEnv("VERIFIER_URL")
	jwtSecret := requireEnv("JWT_SECRET")

	jwtToken, err := testrunner.GenerateJWT(jwtSecret, fixture.Vault.PublicKey, "integration-token-1", 24)
	if err != nil {
		logger.WithError(err).Fatal("failed to generate JWT")
	}

	pluginURL := requireEnv("PLUGIN_ENDPOINT")

	client := testrunner.NewTestClient(verifierURL)
	pluginCli := testrunner.NewTestClient(pluginURL)

	logger.WithFields(logrus.Fields{
		"verifier_url": verifierURL,
		"plugin_url":   pluginURL,
	}).Info("waiting for verifier health")
	err = client.WaitForHealth(60 * time.Second)
	if err != nil {
		logger.WithError(err).Fatal("verifier not healthy")
	}
	logger.Info("verifier is healthy")

	suite := testrunner.NewTestSuite(client, pluginCli, fixture, plugins, jwtToken, logger)
	suite.RunAll()

	if suite.Failed > 0 {
		for _, e := range suite.Errors {
			logger.WithField("error", e).Error("test failure")
		}
		logger.WithFields(logrus.Fields{
			"passed": suite.Passed,
			"failed": suite.Failed,
			"total":  suite.Total,
		}).Fatal("test suite failed")
	}

	logger.WithFields(logrus.Fields{
		"passed": suite.Passed,
		"total":  suite.Total,
	}).Info("all tests passed")
}

func runInstall() {
	fixture, err := testrunner.LoadFixture()
	if err != nil {
		logger.WithError(err).Fatal("failed to load fixture")
	}

	verifierURL := requireEnv("VERIFIER_URL")
	relayURL := requireEnv("RELAY_URL")
	jwtSecret := requireEnv("JWT_SECRET")
	pluginID := requireEnv("PLUGIN_ID")
	encryptionSecret := os.Getenv("ENCRYPTION_SECRET")

	jwtToken, err := testrunner.GenerateJWT(jwtSecret, fixture.Vault.PublicKey, "integration-token-1", 24)
	if err != nil {
		logger.WithError(err).Fatal("failed to generate JWT")
	}

	pluginAPIKey := requireEnv("PLUGIN_API_KEY")
	testTargetAddress := requireEnv("TEST_TARGET_ADDRESS")

	cfg := testrunner.InstallConfig{
		VerifierURL:       verifierURL,
		RelayURL:          relayURL,
		JWTToken:          jwtToken,
		PluginID:          pluginID,
		PluginAPIKey:      pluginAPIKey,
		TestTargetAddress: testTargetAddress,
		Fixture:           fixture,
		EncryptionSecret:  encryptionSecret,
	}

	logger.WithFields(logrus.Fields{
		"verifier_url": verifierURL,
		"relay_url":    relayURL,
		"plugin_id":    pluginID,
	}).Info("starting plugin install (MPC reshare)")

	reshareResult, err := testrunner.RunInstall(cfg, logger)
	if err != nil {
		logger.WithError(err).Fatal("install failed")
	}
	logger.Info("reshare completed successfully")

	logger.Info("starting policy CRUD tests")
	err = testrunner.RunPolicyCRUD(cfg, reshareResult, logger)
	if err != nil {
		logger.WithError(err).Fatal("policy CRUD failed")
	}
	logger.Info("policy CRUD completed successfully")

	logger.Info("install completed successfully")
}

func requireEnv(key string) string {
	val := os.Getenv(key)
	if val == "" {
		logger.WithField("var", key).Fatal("required environment variable is not set")
	}
	return val
}
