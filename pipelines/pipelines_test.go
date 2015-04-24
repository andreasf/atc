package pipelines_test

import (
	"errors"
	"os"

	. "github.com/concourse/atc/pipelines"
	"github.com/concourse/atc/pipelines/fakes"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pivotal-golang/lager/lagertest"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/fake_runner"

	"github.com/concourse/atc/db"
	dbfakes "github.com/concourse/atc/db/fakes"
)

var _ = Describe("Pipelines Syncer", func() {
	var (
		pipelinesDB           *fakes.FakePipelinesDB
		pipelineDB            *dbfakes.FakePipelineDB
		otherPipelineDB       *dbfakes.FakePipelineDB
		pipelineDBFactory     *dbfakes.FakePipelineDBFactory
		pipelineRunnerFactory PipelineRunnerFactory

		fakeRunner         *fake_runner.FakeRunner
		fakeRunnerExitChan chan error
		otherFakeRunner    *fake_runner.FakeRunner

		syncer *Syncer

		// runningPipelines map[string]ifrit.Process
	)

	BeforeEach(func() {
		pipelinesDB = new(fakes.FakePipelinesDB)
		pipelineDB = new(dbfakes.FakePipelineDB)

		pipelineDBFactory = new(dbfakes.FakePipelineDBFactory)

		fakeRunner = new(fake_runner.FakeRunner)
		otherFakeRunner = new(fake_runner.FakeRunner)

		pipelineRunnerFactory = func(pipelineDBArg db.PipelineDB) ifrit.Runner {
			switch pipelineDBArg {
			case pipelineDB:
				return fakeRunner
			case otherPipelineDB:
				return otherFakeRunner
			default:
				panic("unexpected pipelineDB input received")
				return nil
			}
			return fakeRunner
		}

		pipelineDBFactory.BuildStub = func(pipeline db.SavedPipeline) db.PipelineDB {
			switch pipeline.Name {
			case "pipeline":
				return pipelineDB
			case "other-pipeline":
				return otherPipelineDB
			default:
				panic("unexpected pipeline input received")
				return nil
			}
		}

		fakeRunnerExitChan = make(chan error, 1)
		fakeRunner.RunStub = func(signals <-chan os.Signal, ready chan<- struct{}) error {
			close(ready)
			return <-fakeRunnerExitChan
		}

		pipelinesDB.GetAllActivePipelinesReturns([]db.SavedPipeline{
			{
				ID: 1,
				Pipeline: db.Pipeline{
					Name: "pipeline",
				},
			},
			{
				ID: 2,
				Pipeline: db.Pipeline{
					Name: "other-pipeline",
				},
			},
		}, nil)

		syncer = NewSyncer(
			lagertest.NewTestLogger("test"),

			pipelinesDB,
			pipelineDBFactory,
			pipelineRunnerFactory,
		)
	})

	JustBeforeEach(func() {
		syncer.Sync()
	})

	It("spawns a new process for each pipeline", func() {
		Ω(fakeRunner.RunCallCount()).Should(Equal(1))
		Ω(otherFakeRunner.RunCallCount()).Should(Equal(1))
	})

	Context("when we sync again", func() {
		It("does not spawn any processes again", func() {
			syncer.Sync()
			Ω(fakeRunner.RunCallCount()).Should(Equal(1))
		})
	})

	Context("when the pipeline's process exits", func() {
		BeforeEach(func() {
			fakeRunnerExitChan <- nil
		})

		Context("when we sync again", func() {
			It("spawns the process again", func() {
				Ω(fakeRunner.RunCallCount()).Should(Equal(1))
				Ω(otherFakeRunner.RunCallCount()).Should(Equal(1))

				fakeRunnerExitChan <- errors.New("disaster")
				syncer.Sync()

				Ω(fakeRunner.RunCallCount()).Should(Equal(2))
			})
		})
	})

	Context("when the call to lookup pipelines errors", func() {
		It("does not spawn any processes", func() {
		})
	})
})