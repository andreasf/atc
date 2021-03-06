package atccmd

import (
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	_ "net/http/pprof"
	"os"
	"time"

	"github.com/concourse/atc"
	"github.com/concourse/atc/api"
	"github.com/concourse/atc/api/buildserver"
	"github.com/concourse/atc/auth"
	"github.com/concourse/atc/auth/provider"
	"github.com/concourse/atc/builds"
	"github.com/concourse/atc/config"
	"github.com/concourse/atc/db"
	"github.com/concourse/atc/db/migrations"
	"github.com/concourse/atc/engine"
	"github.com/concourse/atc/exec"
	"github.com/concourse/atc/lostandfound"
	"github.com/concourse/atc/metric"
	"github.com/concourse/atc/pipelines"
	"github.com/concourse/atc/radar"
	"github.com/concourse/atc/resource"
	"github.com/concourse/atc/scheduler"
	"github.com/concourse/atc/web"
	"github.com/concourse/atc/web/webhandler"
	"github.com/concourse/atc/worker"
	"github.com/concourse/atc/worker/image"
	"github.com/concourse/atc/wrappa"
	"github.com/dgrijalva/jwt-go"
	"github.com/gorilla/context"
	"github.com/hashicorp/go-multierror"
	"github.com/lib/pq"
	"github.com/pivotal-golang/clock"
	"github.com/pivotal-golang/lager"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/grouper"
	"github.com/tedsuo/ifrit/http_server"
	"github.com/tedsuo/ifrit/sigmon"
	"github.com/xoebus/zest"
)

type ATCCommand struct {
	BindIP   IPFlag `long:"bind-ip"   default:"0.0.0.0" description:"IP address on which to listen for web traffic."`
	BindPort uint16 `long:"bind-port" default:"8080"    description:"Port on which to listen for web traffic."`

	ExternalURL URLFlag `long:"external-url" default:"http://127.0.0.1:8080" description:"URL used to reach any ATC from the outside world."`
	PeerURL     URLFlag `long:"peer-url"     default:"http://127.0.0.1:8080" description:"URL used to reach this ATC from other ATCs in the cluster."`

	OAuthBaseURL URLFlag `long:"oauth-base-url" description:"URL used as the base of OAuth redirect URIs. If not specified, the external URL is used."`

	PostgresDataSource string `long:"postgres-data-source" default:"postgres://127.0.0.1:5432/atc?sslmode=disable" description:"PostgreSQL connection string."`

	DebugBindIP   IPFlag `long:"debug-bind-ip"   default:"127.0.0.1" description:"IP address on which to listen for the pprof debugger endpoints."`
	DebugBindPort uint16 `long:"debug-bind-port" default:"8079"      description:"Port on which to listen for the pprof debugger endpoints."`

	PubliclyViewable bool `short:"p" long:"publicly-viewable" default:"false" description:"If true, anonymous users can view pipelines and public jobs."`

	SessionSigningKey FileFlag `long:"session-signing-key" description:"File containing an RSA private key, used to sign session tokens."`

	ResourceCheckingInterval     time.Duration `long:"resource-checking-interval" default:"1m" description:"Interval on which to check for new versions of resources."`
	OldResourceGracePeriod       time.Duration `long:"old-resource-grace-period" default:"5m" description:"How long to cache the result of a get step after a newer version of the resource is found."`
	ResourceCacheCleanupInterval time.Duration `long:"resource-cache-cleanup-interval" default:"30s" description:"Interval on which to cleanup old caches of resources."`

	CLIArtifactsDir DirFlag `long:"cli-artifacts-dir" description:"Directory containing downloadable CLI binaries."`

	Developer struct {
		DevelopmentMode bool `short:"d" long:"development-mode"  description:"Lax security rules to make local development easier."`
		Noop            bool `short:"n" long:"noop"              description:"Don't actually do any automatic scheduling or checking."`
	} `group:"Developer Options"`

	Worker struct {
		GardenURL       URLFlag            `long:"garden-url"       description:"A Garden API endpoint to register as a worker."`
		BaggageclaimURL URLFlag            `long:"baggageclaim-url" description:"A Baggageclaim API endpoint to register with the worker."`
		ResourceTypes   map[string]URLFlag `long:"resource"         description:"A resource type to advertise for the worker. Can be specified multiple times." value-name:"TYPE:IMAGE"`
	} `group:"Static Worker (optional)" namespace:"worker"`

	BasicAuth struct {
		Username string `long:"username" description:"Username to use for basic auth."`
		Password string `long:"password" description:"Password to use for basic auth."`
	} `group:"Basic Authentication" namespace:"basic-auth"`

	GitHubAuth struct {
		ClientID      string           `long:"client-id"     description:"Application client ID for enabling GitHub OAuth."`
		ClientSecret  string           `long:"client-secret" description:"Application client secret for enabling GitHub OAuth."`
		Organizations []string         `long:"organization"  description:"GitHub organization whose members will have access." value-name:"ORG"`
		Teams         []GitHubTeamFlag `long:"team"          description:"GitHub team whose members will have access." value-name:"ORG/TEAM"`
		Users         []string         `long:"user"          description:"GitHub user to permit access." value-name:"LOGIN"`
	} `group:"GitHub Authentication" namespace:"github-auth"`

	Metrics struct {
		HostName   string            `long:"metrics-host-name"   description:"Host string to attach to emitted metrics."`
		Tags       []string          `long:"metrics-tag"         description:"Tag to attach to emitted metrics. Can be specified multiple times." value-name:"TAG"`
		Attributes map[string]string `long:"metrics-attribute"   description:"A key-value attribute to attach to emitted metrics. Can be specified multiple times." value-name:"NAME:VALUE"`

		YellerAPIKey      string `long:"yeller-api-key"     description:"Yeller API key. If specified, all errors logged will be emitted."`
		YellerEnvironment string `long:"yeller-environment" description:"Environment to tag on all Yeller events emitted."`

		RiemannHost string `long:"riemann-host"                description:"Riemann server address to emit metrics to."`
		RiemannPort uint16 `long:"riemann-port" default:"5555" description:"Port of the Riemann server to emit metrics to."`
	} `group:"Metrics & Diagnostics"`
}

func (cmd *ATCCommand) Execute(args []string) error {
	runner, err := cmd.Runner(args)
	if err != nil {
		return err
	}

	return <-ifrit.Invoke(sigmon.New(runner)).Wait()
}

func (cmd *ATCCommand) Runner(args []string) (ifrit.Runner, error) {
	err := cmd.validate()
	if err != nil {
		return nil, err
	}

	logger, reconfigurableSink := cmd.constructLogger()

	cmd.configureMetrics(logger)

	sqlDB, pipelineDBFactory, err := cmd.constructDB(logger)
	if err != nil {
		return nil, err
	}

	trackerFactory := resource.TrackerFactory{DB: sqlDB}
	workerClient := cmd.constructWorkerPool(logger, sqlDB, trackerFactory)

	tracker := resource.NewTracker(workerClient, sqlDB)
	engine := cmd.constructEngine(sqlDB, workerClient, tracker, cmd.ExternalURL.String())

	radarSchedulerFactory := pipelines.NewRadarSchedulerFactory(
		tracker,
		cmd.ResourceCheckingInterval,
		engine,
		sqlDB,
	)

	signingKey, err := cmd.loadOrGenerateSigningKey()
	if err != nil {
		return nil, err
	}

	err = sqlDB.CreateDefaultTeamIfNotExists()
	if err != nil {
		return nil, err
	}

	authValidator := cmd.constructValidator(signingKey, sqlDB)

	err = cmd.updateBasicAuthCredentials(sqlDB)
	if err != nil {
		return nil, err
	}

	jwtReader := auth.JWTReader{
		PublicKey: &signingKey.PublicKey,
	}

	err = cmd.configureOAuthProviders(logger, sqlDB)
	if err != nil {
		return nil, err
	}

	providerFactory := provider.NewOAuthFactory(
		sqlDB,
		cmd.oauthBaseURL(),
		auth.OAuthRoutes,
		auth.OAuthCallback,
	)
	if err != nil {
		return nil, err
	}

	drain := make(chan struct{})

	apiHandler, err := cmd.constructAPIHandler(
		logger,
		reconfigurableSink,
		sqlDB,
		authValidator,
		jwtReader,
		providerFactory,
		signingKey,
		pipelineDBFactory,
		engine,
		workerClient,
		drain,
		radarSchedulerFactory,
	)
	if err != nil {
		return nil, err
	}

	oauthHandler, err := auth.NewOAuthHandler(
		logger,
		providerFactory,
		signingKey,
		sqlDB,
	)
	if err != nil {
		return nil, err
	}

	webHandler, err := cmd.constructWebHandler(
		logger,
		authValidator,
		jwtReader,
		pipelineDBFactory,
	)
	if err != nil {
		return nil, err
	}

	members := []grouper.Member{
		{"drainer", drainer(drain)},

		{"web", http_server.New(
			cmd.bindAddr(),
			cmd.constructHTTPHandler(
				webHandler,
				apiHandler,
				oauthHandler,
			),
		)},

		{"debug", http_server.New(
			cmd.debugBindAddr(),
			http.DefaultServeMux,
		)},

		{"pipelines", pipelines.SyncRunner{
			Syncer: cmd.constructPipelineSyncer(
				logger.Session("syncer"),
				sqlDB,
				pipelineDBFactory,
				radarSchedulerFactory,
			),
			Interval: 10 * time.Second,
			Clock:    clock.NewClock(),
		}},

		{"builds", builds.TrackerRunner{
			Tracker: builds.NewTracker(
				logger.Session("build-tracker"),
				sqlDB,
				engine,
			),
			Interval: 10 * time.Second,
			Clock:    clock.NewClock(),
		}},

		{"lostandfound", lostandfound.NewRunner(
			logger.Session("lost-and-found"),
			lostandfound.NewBaggageCollector(
				logger.Session("baggage-collector"),
				workerClient,
				sqlDB,
				pipelineDBFactory,
				cmd.OldResourceGracePeriod,
				24*time.Hour,
			),
			sqlDB,
			clock.NewClock(),
			cmd.ResourceCacheCleanupInterval,
		)},
	}

	members = cmd.appendStaticWorker(logger, sqlDB, members)

	return onReady(grouper.NewParallel(os.Interrupt, members), func() {
		logger.Info("listening", lager.Data{
			"web":   cmd.bindAddr(),
			"debug": cmd.debugBindAddr(),
		})
	}), nil
}

func onReady(runner ifrit.Runner, cb func()) ifrit.Runner {
	return ifrit.RunFunc(func(signals <-chan os.Signal, ready chan<- struct{}) error {
		process := ifrit.Background(runner)

		subExited := process.Wait()
		subReady := process.Ready()

		for {
			select {
			case <-subReady:
				cb()
				subReady = nil
			case err := <-subExited:
				return err
			case sig := <-signals:
				process.Signal(sig)
			}
		}
	})
}

func (cmd *ATCCommand) oauthBaseURL() string {
	baseURL := cmd.OAuthBaseURL.String()
	if baseURL == "" {
		baseURL = cmd.ExternalURL.String()
	}
	return baseURL
}

func (cmd *ATCCommand) authConfigured() bool {
	return cmd.basicAuthConfigured() || cmd.gitHubAuthConfigured()
}

func (cmd *ATCCommand) basicAuthConfigured() bool {
	return cmd.BasicAuth.Username != "" || cmd.BasicAuth.Password != ""
}

func (cmd *ATCCommand) gitHubAuthConfigured() bool {
	return len(cmd.GitHubAuth.Organizations) > 0 ||
		len(cmd.GitHubAuth.Teams) > 0 ||
		len(cmd.GitHubAuth.Users) > 0
}

func (cmd *ATCCommand) validate() error {
	var errs *multierror.Error

	if !cmd.authConfigured() && !cmd.Developer.DevelopmentMode {
		errs = multierror.Append(
			errs,
			errors.New("must configure basic auth, OAuth, or turn on development mode"),
		)
	}

	if cmd.gitHubAuthConfigured() {
		if cmd.ExternalURL.URL() == nil {
			errs = multierror.Append(
				errs,
				errors.New("must specify --external-url to use OAuth"),
			)
		}

		if cmd.GitHubAuth.ClientID == "" || cmd.GitHubAuth.ClientSecret == "" {
			errs = multierror.Append(
				errs,
				errors.New("must specify --github-auth-client-id and --github-auth-client-secret to use GitHub OAuth"),
			)
		}
	}

	if cmd.basicAuthConfigured() {
		if cmd.BasicAuth.Username == "" {
			errs = multierror.Append(
				errs,
				errors.New("must specify --basic-auth-username to use basic auth"),
			)
		}
		if cmd.BasicAuth.Password == "" {
			errs = multierror.Append(
				errs,
				errors.New("must specify --basic-auth-password to use basic auth"),
			)
		}
	}

	return errs.ErrorOrNil()
}

func (cmd *ATCCommand) bindAddr() string {
	return fmt.Sprintf("%s:%d", cmd.BindIP, cmd.BindPort)
}

func (cmd *ATCCommand) debugBindAddr() string {
	return fmt.Sprintf("%s:%d", cmd.DebugBindIP, cmd.DebugBindPort)
}

func (cmd *ATCCommand) constructLogger() (lager.Logger, *lager.ReconfigurableSink) {
	logger := lager.NewLogger("atc")

	logLevel := lager.INFO
	if cmd.Developer.DevelopmentMode {
		logLevel = lager.DEBUG
	}

	reconfigurableSink := lager.NewReconfigurableSink(lager.NewWriterSink(os.Stdout, lager.DEBUG), logLevel)
	logger.RegisterSink(reconfigurableSink)

	if cmd.Metrics.YellerAPIKey != "" {
		yellerSink := zest.NewYellerSink(cmd.Metrics.YellerAPIKey, cmd.Metrics.YellerEnvironment)
		logger.RegisterSink(yellerSink)
	}

	return logger, reconfigurableSink
}

func (cmd *ATCCommand) configureMetrics(logger lager.Logger) {
	if cmd.Metrics.RiemannHost != "" {
		host := cmd.Metrics.HostName
		if host == "" {
			host, _ = os.Hostname()
		}

		metric.Initialize(
			logger.Session("metrics"),
			fmt.Sprintf("%s:%d", cmd.Metrics.RiemannHost, cmd.Metrics.RiemannPort),
			host,
			cmd.Metrics.Tags,
			cmd.Metrics.Attributes,
		)
	}
}

func (cmd *ATCCommand) constructDB(logger lager.Logger) (*db.SQLDB, db.PipelineDBFactory, error) {
	dbConn, err := migrations.LockDBAndMigrate(logger.Session("db.migrations"), "postgres", cmd.PostgresDataSource)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to migrate database: %s", err)
	}

	listener := pq.NewListener(cmd.PostgresDataSource, time.Second, time.Minute, nil)
	bus := db.NewNotificationsBus(listener, dbConn)

	explainDBConn := db.Explain(logger, dbConn, clock.NewClock(), 500*time.Millisecond)
	countingDBConn := metric.CountQueries(explainDBConn)
	sqlDB := db.NewSQL(logger.Session("db"), countingDBConn, bus)

	pipelineDBFactory := db.NewPipelineDBFactory(logger.Session("db"), explainDBConn, bus, sqlDB)

	return sqlDB, pipelineDBFactory, err
}

func (cmd *ATCCommand) constructWorkerPool(logger lager.Logger, sqlDB *db.SQLDB, trackerFactory resource.TrackerFactory) worker.Client {
	return worker.NewPool(
		worker.NewDBWorkerProvider(
			logger,
			sqlDB,
			keepaliveDialer,
			worker.ExponentialRetryPolicy{
				Timeout: 5 * time.Minute,
			},
			image.NewFetcher(trackerFactory),
		),
	)
}

func (cmd *ATCCommand) loadOrGenerateSigningKey() (*rsa.PrivateKey, error) {
	var signingKey *rsa.PrivateKey

	if cmd.SessionSigningKey == "" {
		generatedKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return nil, fmt.Errorf("failed to generate session signing key: %s", err)
		}

		signingKey = generatedKey
	} else {
		rsaKeyBlob, err := ioutil.ReadFile(string(cmd.SessionSigningKey))
		if err != nil {
			return nil, fmt.Errorf("failed to read session signing key file: %s", err)
		}

		signingKey, err = jwt.ParseRSAPrivateKeyFromPEM(rsaKeyBlob)
		if err != nil {
			return nil, fmt.Errorf("failed to parse session signing key as RSA: %s", err)
		}
	}

	return signingKey, nil
}

func (cmd *ATCCommand) configureOAuthProviders(logger lager.Logger, sqlDB db.DB) error {
	var err error
	team := db.Team{
		Name: atc.DefaultTeamName,
	}

	gitHubTeams := []db.GitHubTeam{}
	for _, gitHubTeam := range cmd.GitHubAuth.Teams {
		gitHubTeams = append(gitHubTeams, db.GitHubTeam{
			TeamName:         gitHubTeam.TeamName,
			OrganizationName: gitHubTeam.OrganizationName,
		})
	}

	if len(cmd.GitHubAuth.Organizations) > 0 ||
		len(gitHubTeams) > 0 ||
		len(cmd.GitHubAuth.Users) > 0 {
		gitHubAuth := db.GitHubAuth{
			ClientID:      cmd.GitHubAuth.ClientID,
			ClientSecret:  cmd.GitHubAuth.ClientSecret,
			Organizations: cmd.GitHubAuth.Organizations,
			Teams:         gitHubTeams,
			Users:         cmd.GitHubAuth.Users,
		}
		team.GitHubAuth = gitHubAuth
	} else {
		team.GitHubAuth = db.GitHubAuth{}
	}

	_, err = sqlDB.UpdateTeamGitHubAuth(team)
	if err != nil {
		return err
	}

	return nil
}

func (cmd *ATCCommand) constructValidator(signingKey *rsa.PrivateKey, sqlDB db.DB) auth.Validator {
	if !cmd.authConfigured() {
		return auth.NoopValidator{}
	}

	jwtValidator := auth.JWTValidator{
		PublicKey: &signingKey.PublicKey,
	}

	var validator auth.Validator
	if cmd.BasicAuth.Username != "" && cmd.BasicAuth.Password != "" {
		validator = auth.ValidatorBasket{
			auth.BasicAuthValidator{
				DB: sqlDB,
			},
			jwtValidator,
		}
	} else {
		validator = jwtValidator
	}

	return validator
}

func (cmd *ATCCommand) updateBasicAuthCredentials(sqlDB db.DB) error {
	var team db.Team
	if cmd.BasicAuth.Username != "" && cmd.BasicAuth.Password != "" {
		team = db.Team{
			Name: atc.DefaultTeamName,
			BasicAuth: db.BasicAuth{
				BasicAuthUsername: cmd.BasicAuth.Username,
				BasicAuthPassword: cmd.BasicAuth.Password,
			},
		}
	} else {
		team = db.Team{Name: atc.DefaultTeamName}
	}

	_, err := sqlDB.UpdateTeamBasicAuth(team)
	return err
}

func (cmd *ATCCommand) constructEngine(
	sqlDB *db.SQLDB,
	workerClient worker.Client,
	tracker resource.Tracker,
	externalUrl string,
) engine.Engine {
	gardenFactory := exec.NewGardenFactory(workerClient, tracker)

	execV2Engine := engine.NewExecEngine(
		gardenFactory,
		engine.NewBuildDelegateFactory(sqlDB),
		sqlDB,
		externalUrl,
	)

	execV1Engine := engine.NewExecV1DummyEngine()

	return engine.NewDBEngine(engine.Engines{execV2Engine, execV1Engine}, sqlDB)
}

func (cmd *ATCCommand) constructHTTPHandler(
	webHandler http.Handler,
	apiHandler http.Handler,
	oauthHandler http.Handler,
) http.Handler {
	webMux := http.NewServeMux()
	webMux.Handle("/api/v1/", apiHandler)
	webMux.Handle("/auth/", oauthHandler)
	webMux.Handle("/", webHandler)

	var httpHandler http.Handler

	httpHandler = webMux

	// proxy Authorization header to/from auth cookie,
	// to support auth from JS (EventSource) and custom JWT auth
	httpHandler = auth.CookieSetHandler{
		Handler: httpHandler,
	}

	// don't leak gorilla context per-request
	httpHandler = context.ClearHandler(httpHandler)

	return httpHandler
}

func (cmd *ATCCommand) constructAPIHandler(
	logger lager.Logger,
	reconfigurableSink *lager.ReconfigurableSink,
	sqlDB *db.SQLDB,
	authValidator auth.Validator,
	userContextReader auth.UserContextReader,
	providerFactory provider.OAuthFactory,
	signingKey *rsa.PrivateKey,
	pipelineDBFactory db.PipelineDBFactory,
	engine engine.Engine,
	workerClient worker.Client,
	drain <-chan struct{},
	radarSchedulerFactory pipelines.RadarSchedulerFactory,
) (http.Handler, error) {
	apiWrapper := wrappa.MultiWrappa{
		wrappa.NewAPIAuthWrappa(cmd.PubliclyViewable, authValidator, userContextReader),
		wrappa.NewAPIMetricsWrappa(logger),
	}

	return api.NewHandler(
		logger,
		cmd.ExternalURL.String(),
		apiWrapper,

		auth.NewTokenGenerator(signingKey),
		providerFactory,
		cmd.oauthBaseURL(),

		pipelineDBFactory,

		sqlDB, // authserver.AuthDB
		sqlDB, // db.ConfigDB
		sqlDB, // buildserver.BuildsDB
		sqlDB, // workerserver.WorkerDB
		sqlDB, // containerserver.ContainerDB
		sqlDB, // volumeserver.VolumesDB
		sqlDB, // pipes.PipeDB
		sqlDB, // db.PipelinesDB
		sqlDB, // teamserver.TeamDB

		config.ValidateConfig,
		cmd.PeerURL.String(),
		buildserver.NewEventHandler,
		drain,

		engine,
		workerClient,
		radarSchedulerFactory,

		reconfigurableSink,

		cmd.CLIArtifactsDir.Path(),
	)
}

func (cmd *ATCCommand) constructWebHandler(
	logger lager.Logger,
	authValidator auth.Validator,
	userContextReader auth.UserContextReader,
	pipelineDBFactory db.PipelineDBFactory,
) (http.Handler, error) {
	webWrapper := wrappa.MultiWrappa{
		wrappa.NewWebAuthWrappa(authValidator, userContextReader),
		wrappa.NewWebMetricsWrappa(logger),
	}

	clientFactory := web.NewClientFactory(
		fmt.Sprintf("http://127.0.0.1:%d", cmd.BindPort),
	)

	return webhandler.NewHandler(
		logger,
		webWrapper,
		clientFactory,
	)
}

func (cmd *ATCCommand) constructPipelineSyncer(
	logger lager.Logger,
	sqlDB *db.SQLDB,
	pipelineDBFactory db.PipelineDBFactory,
	radarSchedulerFactory pipelines.RadarSchedulerFactory,
) *pipelines.Syncer {
	return pipelines.NewSyncer(
		logger.Session("syncer"),
		sqlDB,
		pipelineDBFactory,
		func(pipelineDB db.PipelineDB) ifrit.Runner {
			return grouper.NewParallel(os.Interrupt, grouper.Members{
				{
					pipelineDB.ScopedName("radar"),
					radar.NewRunner(
						logger.Session(pipelineDB.ScopedName("radar")),
						cmd.Developer.Noop,
						radarSchedulerFactory.BuildRadar(pipelineDB),
						pipelineDB,
						1*time.Minute,
					),
				},
				{
					pipelineDB.ScopedName("scheduler"),
					&scheduler.Runner{
						Logger: logger.Session(pipelineDB.ScopedName("scheduler")),

						DB: pipelineDB,

						Scheduler: radarSchedulerFactory.BuildScheduler(pipelineDB),

						Noop: cmd.Developer.Noop,

						Interval: 10 * time.Second,
					},
				},
			})
		},
	)
}

func (cmd *ATCCommand) appendStaticWorker(
	logger lager.Logger,
	sqlDB *db.SQLDB,
	members []grouper.Member,
) []grouper.Member {
	if cmd.Worker.GardenURL.URL() == nil {
		return members
	}

	var resourceTypes []atc.WorkerResourceType
	for t, u := range cmd.Worker.ResourceTypes {
		resourceTypes = append(resourceTypes, atc.WorkerResourceType{
			Type:  t,
			Image: u.String(),
		})
	}

	return append(members,
		grouper.Member{
			Name: "static-worker",
			Runner: worker.NewHardcoded(
				logger,
				sqlDB,
				clock.NewClock(),
				cmd.Worker.GardenURL.URL().Host,
				cmd.Worker.BaggageclaimURL.String(),
				resourceTypes,
			),
		},
	)
}
