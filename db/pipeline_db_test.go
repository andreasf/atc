package db_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/concourse/atc"
	"github.com/concourse/atc/config"
	"github.com/concourse/atc/db"
	"github.com/concourse/atc/db/algorithm"
	"github.com/concourse/atc/event"
	"github.com/lib/pq"
	"github.com/pivotal-golang/lager/lagertest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func getCheckOrderedVersionedResources(resourceName string, resourceType string, conn db.Conn) ([]db.SavedVersionedResource, error) {
	var svr db.SavedVersionedResource
	var svrs []db.SavedVersionedResource
	rows, err := conn.Query(`
					SELECT vr.resource_id, vr.version, vr.check_order
					FROM versioned_resources vr, resources r
					WHERE r.name = $1
					AND vr.type = $2
					ORDER BY check_order ASC
				`, resourceName, resourceType)

	if err != nil {
		return []db.SavedVersionedResource{}, err
	}

	var version string

	for rows.Next() {
		fmt.Println("wat??")
		err = rows.Scan(&svr.Resource, &version, &svr.CheckOrder)
		if err != nil {
			return []db.SavedVersionedResource{}, err
		}

		fmt.Sprintf("makin a thing w/ version: %+v", version)

		err = json.Unmarshal([]byte(version), &svr.Version)
		if err != nil {
			return []db.SavedVersionedResource{}, err
		}

		svrs = append(svrs, svr)
	}

	return svrs, nil
}

var _ = Describe("PipelineDB", func() {
	var dbConn db.Conn
	var listener *pq.Listener

	var pipelineDBFactory db.PipelineDBFactory
	var sqlDB *db.SQLDB

	BeforeEach(func() {
		postgresRunner.Truncate()

		dbConn = db.Wrap(postgresRunner.Open())

		listener = pq.NewListener(postgresRunner.DataSourceName(), time.Second, time.Minute, nil)
		Eventually(listener.Ping, 5*time.Second).ShouldNot(HaveOccurred())
		bus := db.NewNotificationsBus(listener, dbConn)

		sqlDB = db.NewSQL(lagertest.NewTestLogger("test"), dbConn, bus)
		pipelineDBFactory = db.NewPipelineDBFactory(lagertest.NewTestLogger("test"), dbConn, bus, sqlDB)
	})

	AfterEach(func() {
		err := dbConn.Close()
		Expect(err).NotTo(HaveOccurred())

		err = listener.Close()
		Expect(err).NotTo(HaveOccurred())
	})

	pipelineConfig := atc.Config{
		Groups: atc.GroupConfigs{
			{
				Name:      "some-group",
				Jobs:      []string{"job-1", "job-2"},
				Resources: []string{"some-resource", "some-other-resource"},
			},
		},

		Resources: atc.ResourceConfigs{
			{
				Name: "some-resource",
				Type: "some-type",
				Source: atc.Source{
					"source-config": "some-value",
				},
			},
			{
				Name: "some-other-resource",
				Type: "some-type",
				Source: atc.Source{
					"source-config": "some-value",
				},
			},
			{
				Name: "some-really-other-resource",
				Type: "some-type",
				Source: atc.Source{
					"source-config": "some-value",
				},
			},
		},

		Jobs: atc.JobConfigs{
			{
				Name: "some-job",

				Public: true,

				Serial: true,

				SerialGroups: []string{"serial-group"},

				Plan: atc.PlanSequence{
					{
						Put: "some-resource",
						Params: atc.Params{
							"some-param": "some-value",
						},
					},
					{
						Get:      "some-input",
						Resource: "some-resource",
						Params: atc.Params{
							"some-param": "some-value",
						},
						Passed:  []string{"job-1", "job-2"},
						Trigger: true,
					},
					{
						Task:           "some-task",
						Privileged:     true,
						TaskConfigPath: "some/config/path.yml",
						TaskConfig: &atc.TaskConfig{
							Image: "some-image",
						},
					},
				},
			},
			{
				Name:   "some-other-job",
				Serial: true,
			},
			{
				Name: "a-job",
			},
			{
				Name: "shared-job",
			},
			{
				Name: "random-job",
			},
			{
				Name:         "other-serial-group-job",
				SerialGroups: []string{"serial-group"},
			},
			{
				Name:         "different-serial-group-job",
				SerialGroups: []string{"different-serial-group"},
			},
		},
	}

	otherPipelineConfig := atc.Config{
		Groups: atc.GroupConfigs{
			{
				Name:      "some-group",
				Jobs:      []string{"job-1", "job-2"},
				Resources: []string{"some-resource", "some-other-resource"},
			},
		},

		Resources: atc.ResourceConfigs{
			{
				Name: "some-resource",
				Type: "some-type",
				Source: atc.Source{
					"source-config": "some-value",
				},
			},
			{
				Name: "some-other-resource",
				Type: "some-type",
				Source: atc.Source{
					"source-config": "some-value",
				},
			},
		},

		Jobs: atc.JobConfigs{
			{
				Name: "some-job",
			},
			{
				Name: "some-other-job",
			},
			{
				Name: "a-job",
			},
			{
				Name: "shared-job",
			},
		},
	}

	var (
		team            db.SavedTeam
		pipelineDB      db.PipelineDB
		otherPipelineDB db.PipelineDB
	)

	BeforeEach(func() {
		var err error
		team, err = sqlDB.SaveTeam(db.Team{Name: "some-team"})
		Expect(err).NotTo(HaveOccurred())

		_, _, err = sqlDB.SaveConfig(team.Name, "a-pipeline-name", pipelineConfig, 0, db.PipelineUnpaused)
		Expect(err).NotTo(HaveOccurred())
		savedPipeline, err := sqlDB.GetPipelineByTeamNameAndName(team.Name, "a-pipeline-name")
		Expect(err).NotTo(HaveOccurred())

		_, _, err = sqlDB.SaveConfig(team.Name, "other-pipeline-name", otherPipelineConfig, 0, db.PipelineUnpaused)
		Expect(err).NotTo(HaveOccurred())
		otherSavedPipeline, err := sqlDB.GetPipelineByTeamNameAndName(team.Name, "other-pipeline-name")
		Expect(err).NotTo(HaveOccurred())

		pipelineDB = pipelineDBFactory.Build(savedPipeline)
		otherPipelineDB = pipelineDBFactory.Build(otherSavedPipeline)
	})

	loadAndGetLatestInputVersions := func(jobName string, inputs []config.JobInput) ([]db.BuildInput, bool, error) {
		versions, err := pipelineDB.LoadVersionsDB()
		if err != nil {
			return nil, false, err
		}

		return pipelineDB.GetLatestInputVersions(versions, jobName, inputs)
	}

	Describe("destroying a pipeline", func() {
		It("can be deleted", func() {
			// populate pipelines table
			_, _, err := sqlDB.SaveConfig(team.Name, "a-pipeline-that-will-be-deleted", pipelineConfig, 0, db.PipelineUnpaused)
			Expect(err).NotTo(HaveOccurred())

			fetchedPipeline, err := sqlDB.GetPipelineByTeamNameAndName(team.Name, "a-pipeline-that-will-be-deleted")
			Expect(err).NotTo(HaveOccurred())

			fetchedPipelineDB := pipelineDBFactory.Build(fetchedPipeline)

			// populate resources table and versioned_resources table

			savedResource, err := fetchedPipelineDB.GetResource("some-resource")
			Expect(err).NotTo(HaveOccurred())

			resourceConfig, found := pipelineConfig.Resources.Lookup("some-resource")
			Expect(found).To(BeTrue())

			fetchedPipelineDB.SaveResourceVersions(resourceConfig, []atc.Version{
				{
					"key": "value",
				},
			})

			// populate builds table
			build, err := fetchedPipelineDB.CreateJobBuild("some-job")
			Expect(err).NotTo(HaveOccurred())

			oneOffBuild, err := sqlDB.CreateOneOffBuild()
			Expect(err).NotTo(HaveOccurred())

			// populate jobs_serial_groups table
			_, err = fetchedPipelineDB.GetRunningBuildsBySerialGroup("some-job", []string{"serial-group"})
			Expect(err).NotTo(HaveOccurred())

			// populate build_inputs table
			_, err = fetchedPipelineDB.SaveBuildInput(build.ID, db.BuildInput{
				Name: "build-input",
				VersionedResource: db.VersionedResource{
					Resource:     "some-resource",
					PipelineName: fetchedPipeline.Name,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// In very old concourse deployments, build inputs and outputs seem to
			// have been created for one-off builds. This test makes sure they get
			// deleted. See story #109558152
			_, err = fetchedPipelineDB.SaveBuildInput(oneOffBuild.ID, db.BuildInput{
				Name: "one-off-build-input",
				VersionedResource: db.VersionedResource{
					Resource:     "some-resource",
					PipelineName: "a-pipeline-that-will-be-deleted",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// populate build_outputs table

			_, err = fetchedPipelineDB.SaveBuildOutput(build.ID, db.VersionedResource{
				Resource:     "some-resource",
				PipelineName: "a-pipeline-that-will-be-deleted",
			}, false)
			Expect(err).NotTo(HaveOccurred())

			_, err = fetchedPipelineDB.SaveBuildOutput(oneOffBuild.ID, db.VersionedResource{
				Resource:     "some-resource",
				PipelineName: "a-pipeline-that-will-be-deleted",
			}, false)
			Expect(err).NotTo(HaveOccurred())

			// populate build_events table
			err = sqlDB.SaveBuildEvent(build.ID, event.StartTask{})
			Expect(err).NotTo(HaveOccurred())

			// populate image_resource_versions table
			err = sqlDB.SaveImageResourceVersion(build.ID, "some-plan-id", db.VolumeIdentifier{
				ResourceVersion: atc.Version{"digest": "readers"},
				ResourceHash:    `docker{"some":"source"}`,
			})
			Expect(err).NotTo(HaveOccurred())

			err = fetchedPipelineDB.Destroy()
			Expect(err).NotTo(HaveOccurred())

			pipelines, err := sqlDB.GetAllPipelines()
			Expect(err).NotTo(HaveOccurred())
			Expect(pipelines).NotTo(ContainElement(fetchedPipeline))

			_, _, found, err = fetchedPipelineDB.GetConfig()
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeFalse())

			resourceRows, err := dbConn.Query(`select id from resources where pipeline_id = $1`, fetchedPipeline.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(resourceRows.Next()).To(BeFalse())

			versionRows, err := dbConn.Query(`select id from versioned_resources where resource_id = $1`, savedResource.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(versionRows.Next()).To(BeFalse())

			buildRows, err := dbConn.Query(`select id from builds where id = $1`, build.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(buildRows.Next()).To(BeFalse())

			jobRows, err := dbConn.Query(`select id from jobs where pipeline_id = $1`, fetchedPipeline.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(jobRows.Next()).To(BeFalse())

			eventRows, err := dbConn.Query(`select build_id from build_events where build_id = $1`, build.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(eventRows.Next()).To(BeFalse())

			inputRows, err := dbConn.Query(`select build_id from build_inputs where build_id = $1`, build.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(inputRows.Next()).To(BeFalse())

			oneOffInputRows, err := dbConn.Query(`select build_id from build_inputs where build_id = $1`, oneOffBuild.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(oneOffInputRows.Next()).To(BeFalse())

			outputRows, err := dbConn.Query(`select build_id from build_outputs where build_id = $1`, build.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(outputRows.Next()).To(BeFalse())

			oneOffOutputRows, err := dbConn.Query(`select build_id from build_outputs where build_id = $1`, oneOffBuild.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(oneOffOutputRows.Next()).To(BeFalse())

			foundImageVolumeIdentifiers, err := sqlDB.GetImageVolumeIdentifiersByBuildID(build.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(foundImageVolumeIdentifiers).To(BeEmpty())
		})
	})

	Describe("pausing and unpausing a pipeline", func() {
		It("starts out as unpaused", func() {
			pipeline, err := sqlDB.GetPipelineByTeamNameAndName(team.Name, "a-pipeline-name")
			Expect(err).NotTo(HaveOccurred())

			Expect(pipeline.Paused).To(BeFalse())
		})

		It("can be paused", func() {
			err := pipelineDB.Pause()
			Expect(err).NotTo(HaveOccurred())

			pipelinePaused, err := pipelineDB.IsPaused()
			Expect(err).NotTo(HaveOccurred())
			Expect(pipelinePaused).To(BeTrue())

			otherPipelinePaused, err := otherPipelineDB.IsPaused()
			Expect(err).NotTo(HaveOccurred())
			Expect(otherPipelinePaused).To(BeFalse())
		})

		It("can be unpaused", func() {
			err := pipelineDB.Pause()
			Expect(err).NotTo(HaveOccurred())

			err = otherPipelineDB.Pause()
			Expect(err).NotTo(HaveOccurred())

			err = pipelineDB.Unpause()
			Expect(err).NotTo(HaveOccurred())

			pipelinePaused, err := pipelineDB.IsPaused()
			Expect(err).NotTo(HaveOccurred())
			Expect(pipelinePaused).To(BeFalse())

			otherPipelinePaused, err := otherPipelineDB.IsPaused()
			Expect(err).NotTo(HaveOccurred())
			Expect(otherPipelinePaused).To(BeTrue())
		})
	})

	Describe("ScopedName", func() {
		It("concatenates the pipeline name with the passed in name", func() {
			pipelineDB := pipelineDBFactory.Build(db.SavedPipeline{
				Pipeline: db.Pipeline{
					Name: "some-pipeline",
				},
			})
			Expect(pipelineDB.ScopedName("something-else")).To(Equal("some-pipeline:something-else"))
		})
	})

	Describe("getting the pipeline configuration", func() {
		It("can manage multiple pipeline configurations", func() {
			By("returning the saved config to later gets")
			returnedConfig, configVersion, found, err := pipelineDB.GetConfig()
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(returnedConfig).To(Equal(pipelineConfig))
			Expect(configVersion).NotTo(Equal(db.ConfigVersion(0)))

			otherReturnedConfig, otherConfigVersion, found, err := otherPipelineDB.GetConfig()
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(otherReturnedConfig).To(Equal(otherPipelineConfig))
			Expect(otherConfigVersion).NotTo(Equal(db.ConfigVersion(0)))

			updatedConfig := pipelineConfig

			updatedConfig.Groups = append(pipelineConfig.Groups, atc.GroupConfig{
				Name: "new-group",
				Jobs: []string{"new-job-1", "new-job-2"},
			})

			updatedConfig.Resources = append(pipelineConfig.Resources, atc.ResourceConfig{
				Name: "new-resource",
				Type: "new-type",
				Source: atc.Source{
					"new-source-config": "new-value",
				},
			})

			updatedConfig.Jobs = append(pipelineConfig.Jobs, atc.JobConfig{
				Name: "new-job",
				Plan: atc.PlanSequence{
					{
						Get:      "new-input",
						Resource: "new-resource",
						Params: atc.Params{
							"new-param": "new-value",
						},
					},
					{
						Task:           "some-task",
						TaskConfigPath: "new/config/path.yml",
					},
				},
			})

			By("being able to update the config with a valid config")
			_, _, err = sqlDB.SaveConfig(team.Name, "a-pipeline-name", updatedConfig, configVersion, db.PipelineUnpaused)
			Expect(err).NotTo(HaveOccurred())
			_, _, err = sqlDB.SaveConfig(team.Name, "other-pipeline-name", updatedConfig, otherConfigVersion, db.PipelineUnpaused)
			Expect(err).NotTo(HaveOccurred())

			By("returning the updated config")
			returnedConfig, newConfigVersion, found, err := pipelineDB.GetConfig()
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(returnedConfig).To(Equal(updatedConfig))
			Expect(newConfigVersion).NotTo(Equal(configVersion))

			otherReturnedConfig, newOtherConfigVersion, found, err := otherPipelineDB.GetConfig()
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(otherReturnedConfig).To(Equal(updatedConfig))
			Expect(newOtherConfigVersion).NotTo(Equal(otherConfigVersion))
		})
	})

	Context("Resources", func() {
		resourceName := "some-resource"
		otherResourceName := "some-other-resource"
		reallyOtherResourceName := "some-really-other-resource"

		var resource db.SavedResource
		var otherResource db.SavedResource
		var reallyOtherResource db.SavedResource

		BeforeEach(func() {
			var err error
			resource, err = pipelineDB.GetResource(resourceName)
			Expect(err).NotTo(HaveOccurred())

			otherResource, err = pipelineDB.GetResource(otherResourceName)
			Expect(err).NotTo(HaveOccurred())

			reallyOtherResource, err = pipelineDB.GetResource(reallyOtherResourceName)
			Expect(err).NotTo(HaveOccurred())
		})

		Context("SaveResourceVersions", func() {
			var (
				originalVersionSlice []atc.Version
				resourceConfig       atc.ResourceConfig
			)

			BeforeEach(func() {
				resourceConfig = atc.ResourceConfig{
					Name:   resource.Name,
					Type:   "some-type",
					Source: atc.Source{"some": "source"},
				}

				originalVersionSlice = []atc.Version{
					{"ref": "v1"},
					{"ref": "v3"},
				}
			})

			JustBeforeEach(func() {

				err := pipelineDB.SaveResourceVersions(resourceConfig, originalVersionSlice)
				Expect(err).NotTo(HaveOccurred())
			})

			It("ensures versioned resources have the correct check_order", func() {
				latestVR, found, err := pipelineDB.GetLatestVersionedResource(resource)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())

				Expect(latestVR.Version).To(Equal(db.Version{"ref": "v3"}))

				pretendCheckResults := []atc.Version{
					{"ref": "v2"},
					{"ref": "v3"},
				}

				err = pipelineDB.SaveResourceVersions(resourceConfig, pretendCheckResults)
				Expect(err).NotTo(HaveOccurred())

				latestVR, found, err = pipelineDB.GetLatestVersionedResource(resource)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())

				Expect(latestVR.Version).To(Equal(db.Version{"ref": "v3"}))
			})
		})

		It("can load up versioned resource information relevant to scheduling", func() {
			job, err := pipelineDB.GetJob("some-job")
			Expect(err).NotTo(HaveOccurred())

			otherJob, err := pipelineDB.GetJob("some-other-job")
			Expect(err).NotTo(HaveOccurred())

			aJob, err := pipelineDB.GetJob("a-job")
			Expect(err).NotTo(HaveOccurred())

			sharedJob, err := pipelineDB.GetJob("shared-job")
			Expect(err).NotTo(HaveOccurred())

			randomJob, err := pipelineDB.GetJob("random-job")
			Expect(err).NotTo(HaveOccurred())

			otherSerialGroupJob, err := pipelineDB.GetJob("other-serial-group-job")
			Expect(err).NotTo(HaveOccurred())

			differentSerialGroupJob, err := pipelineDB.GetJob("different-serial-group-job")
			Expect(err).NotTo(HaveOccurred())

			versions, err := pipelineDB.LoadVersionsDB()
			Expect(err).NotTo(HaveOccurred())
			Expect(versions.ResourceVersions).To(BeEmpty())
			Expect(versions.BuildOutputs).To(BeEmpty())
			Expect(versions.ResourceIDs).To(Equal(map[string]int{
				resource.Name:            resource.ID,
				otherResource.Name:       otherResource.ID,
				reallyOtherResource.Name: reallyOtherResource.ID,
			}))

			Expect(versions.JobIDs).To(Equal(map[string]int{
				"some-job":                   job.ID,
				"some-other-job":             otherJob.ID,
				"a-job":                      aJob.ID,
				"shared-job":                 sharedJob.ID,
				"random-job":                 randomJob.ID,
				"other-serial-group-job":     otherSerialGroupJob.ID,
				"different-serial-group-job": differentSerialGroupJob.ID,
			}))

			By("initially having no latest versioned resource")
			_, found, err := pipelineDB.GetLatestVersionedResource(resource)
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeFalse())

			By("including saved versioned resources of the current pipeline")
			err = pipelineDB.SaveResourceVersions(atc.ResourceConfig{
				Name:   resource.Name,
				Type:   "some-type",
				Source: atc.Source{"some": "source"},
			}, []atc.Version{{"version": "1"}})
			Expect(err).NotTo(HaveOccurred())

			savedVR1, found, err := pipelineDB.GetLatestVersionedResource(resource)
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(savedVR1.ModifiedTime).NotTo(BeNil())
			Expect(savedVR1.ModifiedTime).To(BeTemporally(">", time.Time{}))

			err = pipelineDB.SaveResourceVersions(atc.ResourceConfig{
				Name:   resource.Name,
				Type:   "some-type",
				Source: atc.Source{"some": "source"},
			}, []atc.Version{{"version": "2"}})
			Expect(err).NotTo(HaveOccurred())

			savedVR2, found, err := pipelineDB.GetLatestVersionedResource(resource)
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())

			versions, err = pipelineDB.LoadVersionsDB()
			Expect(err).NotTo(HaveOccurred())
			Expect(versions.ResourceVersions).To(ConsistOf([]algorithm.ResourceVersion{
				{VersionID: savedVR1.ID, ResourceID: resource.ID},
				{VersionID: savedVR2.ID, ResourceID: resource.ID},
			}))

			Expect(versions.BuildOutputs).To(BeEmpty())
			Expect(versions.ResourceIDs).To(Equal(map[string]int{
				resource.Name:            resource.ID,
				otherResource.Name:       otherResource.ID,
				reallyOtherResource.Name: reallyOtherResource.ID,
			}))

			Expect(versions.JobIDs).To(Equal(map[string]int{
				"some-job":                   job.ID,
				"some-other-job":             otherJob.ID,
				"a-job":                      aJob.ID,
				"shared-job":                 sharedJob.ID,
				"random-job":                 randomJob.ID,
				"other-serial-group-job":     otherSerialGroupJob.ID,
				"different-serial-group-job": differentSerialGroupJob.ID,
			}))

			By("not including saved versioned resources of other pipelines")
			otherPipelineResource, err := otherPipelineDB.GetResource("some-other-resource")
			Expect(err).NotTo(HaveOccurred())

			err = otherPipelineDB.SaveResourceVersions(atc.ResourceConfig{
				Name:   otherPipelineResource.Name,
				Type:   "some-type",
				Source: atc.Source{"some": "source"},
			}, []atc.Version{{"version": "1"}})
			Expect(err).NotTo(HaveOccurred())

			otherPipelineSavedVR, found, err := otherPipelineDB.GetLatestVersionedResource(otherPipelineResource)
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())

			versions, err = pipelineDB.LoadVersionsDB()
			Expect(err).NotTo(HaveOccurred())
			Expect(versions.ResourceVersions).To(ConsistOf([]algorithm.ResourceVersion{
				{VersionID: savedVR1.ID, ResourceID: resource.ID},
				{VersionID: savedVR2.ID, ResourceID: resource.ID},
			}))

			Expect(versions.BuildOutputs).To(BeEmpty())
			Expect(versions.ResourceIDs).To(Equal(map[string]int{
				resource.Name:            resource.ID,
				otherResource.Name:       otherResource.ID,
				reallyOtherResource.Name: reallyOtherResource.ID,
			}))

			Expect(versions.JobIDs).To(Equal(map[string]int{
				"some-job":                   job.ID,
				"some-other-job":             otherJob.ID,
				"a-job":                      aJob.ID,
				"shared-job":                 sharedJob.ID,
				"random-job":                 randomJob.ID,
				"other-serial-group-job":     otherSerialGroupJob.ID,
				"different-serial-group-job": differentSerialGroupJob.ID,
			}))

			By("including outputs of successful builds")
			build1, err := pipelineDB.CreateJobBuild("a-job")
			Expect(err).NotTo(HaveOccurred())

			_, err = pipelineDB.SaveBuildOutput(build1.ID, savedVR1.VersionedResource, false)
			Expect(err).NotTo(HaveOccurred())

			err = sqlDB.FinishBuild(build1.ID, db.StatusSucceeded)
			Expect(err).NotTo(HaveOccurred())

			versions, err = pipelineDB.LoadVersionsDB()
			Expect(err).NotTo(HaveOccurred())
			Expect(versions.ResourceVersions).To(ConsistOf([]algorithm.ResourceVersion{
				{VersionID: savedVR1.ID, ResourceID: resource.ID},
				{VersionID: savedVR2.ID, ResourceID: resource.ID},
			}))

			Expect(versions.BuildOutputs).To(ConsistOf([]algorithm.BuildOutput{
				{
					ResourceVersion: algorithm.ResourceVersion{
						VersionID:  savedVR1.ID,
						ResourceID: resource.ID,
					},
					JobID:   build1.JobID,
					BuildID: build1.ID,
				},
			}))

			Expect(versions.ResourceIDs).To(Equal(map[string]int{
				resource.Name:            resource.ID,
				otherResource.Name:       otherResource.ID,
				reallyOtherResource.Name: reallyOtherResource.ID,
			}))

			Expect(versions.JobIDs).To(Equal(map[string]int{
				"some-job":                   job.ID,
				"a-job":                      build1.JobID,
				"some-other-job":             otherJob.ID,
				"shared-job":                 sharedJob.ID,
				"random-job":                 randomJob.ID,
				"other-serial-group-job":     otherSerialGroupJob.ID,
				"different-serial-group-job": differentSerialGroupJob.ID,
			}))

			By("not including outputs of failed builds")
			build2, err := pipelineDB.CreateJobBuild("a-job")
			Expect(err).NotTo(HaveOccurred())

			_, err = pipelineDB.SaveBuildOutput(build2.ID, savedVR1.VersionedResource, false)
			Expect(err).NotTo(HaveOccurred())

			err = sqlDB.FinishBuild(build2.ID, db.StatusFailed)
			Expect(err).NotTo(HaveOccurred())

			versions, err = pipelineDB.LoadVersionsDB()
			Expect(err).NotTo(HaveOccurred())
			Expect(versions.ResourceVersions).To(ConsistOf([]algorithm.ResourceVersion{
				{VersionID: savedVR1.ID, ResourceID: resource.ID},
				{VersionID: savedVR2.ID, ResourceID: resource.ID},
			}))

			Expect(versions.BuildOutputs).To(ConsistOf([]algorithm.BuildOutput{
				{
					ResourceVersion: algorithm.ResourceVersion{
						VersionID:  savedVR1.ID,
						ResourceID: resource.ID,
					},
					JobID:   build1.JobID,
					BuildID: build1.ID,
				},
			}))

			Expect(versions.ResourceIDs).To(Equal(map[string]int{
				resource.Name:            resource.ID,
				otherResource.Name:       otherResource.ID,
				reallyOtherResource.Name: reallyOtherResource.ID,
			}))

			Expect(versions.JobIDs).To(Equal(map[string]int{
				"some-job":                   job.ID,
				"a-job":                      build1.JobID,
				"some-other-job":             otherJob.ID,
				"shared-job":                 sharedJob.ID,
				"random-job":                 randomJob.ID,
				"other-serial-group-job":     otherSerialGroupJob.ID,
				"different-serial-group-job": differentSerialGroupJob.ID,
			}))

			By("not including outputs of builds in other pipelines")
			otherPipelineBuild, err := otherPipelineDB.CreateJobBuild("a-job")
			Expect(err).NotTo(HaveOccurred())

			_, err = otherPipelineDB.SaveBuildOutput(otherPipelineBuild.ID, otherPipelineSavedVR.VersionedResource, false)
			Expect(err).NotTo(HaveOccurred())

			err = sqlDB.FinishBuild(otherPipelineBuild.ID, db.StatusSucceeded)
			Expect(err).NotTo(HaveOccurred())

			versions, err = pipelineDB.LoadVersionsDB()
			Expect(err).NotTo(HaveOccurred())
			Expect(versions.ResourceVersions).To(ConsistOf([]algorithm.ResourceVersion{
				{VersionID: savedVR1.ID, ResourceID: resource.ID},
				{VersionID: savedVR2.ID, ResourceID: resource.ID},
			}))

			Expect(versions.BuildOutputs).To(ConsistOf([]algorithm.BuildOutput{
				{
					ResourceVersion: algorithm.ResourceVersion{
						VersionID:  savedVR1.ID,
						ResourceID: resource.ID,
					},
					JobID:   build1.JobID,
					BuildID: build1.ID,
				},
			}))

			Expect(versions.ResourceIDs).To(Equal(map[string]int{
				resource.Name:            resource.ID,
				otherResource.Name:       otherResource.ID,
				reallyOtherResource.Name: reallyOtherResource.ID,
			}))

			Expect(versions.JobIDs).To(Equal(map[string]int{
				"some-job":                   job.ID,
				"a-job":                      build1.JobID,
				"some-other-job":             otherJob.ID,
				"shared-job":                 sharedJob.ID,
				"random-job":                 randomJob.ID,
				"other-serial-group-job":     otherSerialGroupJob.ID,
				"different-serial-group-job": differentSerialGroupJob.ID,
			}))
		})

		It("can load up the latest enabled versioned resource", func() {
			By("initially having no latest versioned resource")
			_, found, err := pipelineDB.GetLatestEnabledVersionedResource(resource.Name)
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeFalse())

			By("including saved versioned resources of the current pipeline")
			err = pipelineDB.SaveResourceVersions(atc.ResourceConfig{
				Name:   resource.Name,
				Type:   "some-type",
				Source: atc.Source{"some": "source"},
			}, []atc.Version{{"version": "1"}})
			Expect(err).NotTo(HaveOccurred())

			savedVR1, found, err := pipelineDB.GetLatestEnabledVersionedResource(resource.Name)
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())

			err = pipelineDB.SaveResourceVersions(atc.ResourceConfig{
				Name:   resource.Name,
				Type:   "some-type",
				Source: atc.Source{"some": "source"},
			}, []atc.Version{{"version": "2"}})
			Expect(err).NotTo(HaveOccurred())

			savedVR2, found, err := pipelineDB.GetLatestEnabledVersionedResource(resource.Name)
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())

			Expect(savedVR1.Version).To(Equal(db.Version{"version": "1"}))
			Expect(savedVR2.Version).To(Equal(db.Version{"version": "2"}))

			By("not including saved versioned resources of other pipelines")
			_, err = otherPipelineDB.GetResource("some-other-resource")
			Expect(err).NotTo(HaveOccurred())

			err = otherPipelineDB.SaveResourceVersions(atc.ResourceConfig{
				Name:   resource.Name,
				Type:   "some-type",
				Source: atc.Source{"some": "source"},
			}, []atc.Version{{"version": "3"}})
			Expect(err).NotTo(HaveOccurred())

			otherPipelineSavedVR, found, err := pipelineDB.GetLatestEnabledVersionedResource(resource.Name)
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())

			Expect(otherPipelineSavedVR.Version).To(Equal(db.Version{"version": "2"}))

			err = pipelineDB.DisableVersionedResource(savedVR2.ID)
			Expect(err).NotTo(HaveOccurred())

			savedVR3, found, err := pipelineDB.GetLatestEnabledVersionedResource(resource.Name)
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())

			Expect(savedVR3.Version).To(Equal(db.Version{"version": "1"}))
		})

		Describe("pausing and unpausing resources", func() {
			It("starts out as unpaused", func() {
				resource, err := pipelineDB.GetResource(resourceName)
				Expect(err).NotTo(HaveOccurred())

				Expect(resource.Paused).To(BeFalse())
			})

			It("can be paused", func() {
				err := pipelineDB.PauseResource(resourceName)
				Expect(err).NotTo(HaveOccurred())

				pausedResource, err := pipelineDB.GetResource(resourceName)
				Expect(err).NotTo(HaveOccurred())
				Expect(pausedResource.Paused).To(BeTrue())

				resource, err := otherPipelineDB.GetResource(resourceName)
				Expect(err).NotTo(HaveOccurred())
				Expect(resource.Paused).To(BeFalse())
			})

			It("can be unpaused", func() {
				err := pipelineDB.PauseResource(resourceName)
				Expect(err).NotTo(HaveOccurred())

				err = otherPipelineDB.PauseResource(resourceName)
				Expect(err).NotTo(HaveOccurred())

				err = pipelineDB.UnpauseResource(resourceName)
				Expect(err).NotTo(HaveOccurred())

				unpausedResource, err := pipelineDB.GetResource(resourceName)
				Expect(err).NotTo(HaveOccurred())
				Expect(unpausedResource.Paused).To(BeFalse())

				resource, err := otherPipelineDB.GetResource(resourceName)
				Expect(err).NotTo(HaveOccurred())
				Expect(resource.Paused).To(BeTrue())
			})
		})

		Describe("enabling and disabling versioned resources", func() {
			It("returns an error if the resource or version is bogus", func() {
				err := pipelineDB.EnableVersionedResource(42)
				Expect(err).To(HaveOccurred())

				err = pipelineDB.DisableVersionedResource(42)
				Expect(err).To(HaveOccurred())
			})

			It("does not affect explicitly fetching the latest version", func() {
				err := pipelineDB.SaveResourceVersions(atc.ResourceConfig{
					Name:   "some-resource",
					Type:   "some-type",
					Source: atc.Source{"some": "source"},
				}, []atc.Version{{"version": "1"}})
				Expect(err).NotTo(HaveOccurred())

				savedVR, found, err := pipelineDB.GetLatestVersionedResource(resource)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())

				Expect(savedVR.Resource).To(Equal("some-resource"))
				Expect(savedVR.Type).To(Equal("some-type"))
				Expect(savedVR.Version).To(Equal(db.Version{"version": "1"}))
				initialTime := savedVR.ModifiedTime

				err = pipelineDB.DisableVersionedResource(savedVR.ID)
				Expect(err).NotTo(HaveOccurred())

				disabledVR := savedVR
				disabledVR.Enabled = false

				latestVR, found, err := pipelineDB.GetLatestVersionedResource(resource)
				Expect(found).To(BeTrue())
				Expect(latestVR.Resource).To(Equal(disabledVR.Resource))
				Expect(latestVR.Type).To(Equal(disabledVR.Type))
				Expect(latestVR.Version).To(Equal(disabledVR.Version))
				Expect(latestVR.Enabled).To(BeFalse())
				Expect(latestVR.ModifiedTime).To(BeTemporally(">", initialTime))

				tmp_modified_time := latestVR.ModifiedTime

				err = pipelineDB.EnableVersionedResource(savedVR.ID)
				Expect(err).NotTo(HaveOccurred())

				enabledVR := savedVR
				enabledVR.Enabled = true

				latestVR, found, err = pipelineDB.GetLatestVersionedResource(resource)
				Expect(found).To(BeTrue())
				Expect(latestVR.Resource).To(Equal(enabledVR.Resource))
				Expect(latestVR.Type).To(Equal(enabledVR.Type))
				Expect(latestVR.Version).To(Equal(enabledVR.Version))
				Expect(latestVR.Enabled).To(BeTrue())
				Expect(latestVR.ModifiedTime).To(BeTemporally(">", tmp_modified_time))
			})

			It("prevents the resource version from being eligible as a previous set of inputs", func() {
				err := pipelineDB.SaveResourceVersions(atc.ResourceConfig{
					Name:   "some-resource",
					Type:   "some-type",
					Source: atc.Source{"some": "source"},
				}, []atc.Version{{"version": "1"}})
				Expect(err).NotTo(HaveOccurred())

				savedVR1, found, err := pipelineDB.GetLatestVersionedResource(resource)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())

				otherResource, err := pipelineDB.GetResource("some-other-resource")
				Expect(err).NotTo(HaveOccurred())

				err = pipelineDB.SaveResourceVersions(atc.ResourceConfig{
					Name:   "some-other-resource",
					Type:   "some-type",
					Source: atc.Source{"some": "source"},
				}, []atc.Version{{"version": "1"}})
				Expect(err).NotTo(HaveOccurred())

				otherSavedVR1, found, err := pipelineDB.GetLatestVersionedResource(otherResource)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())

				err = pipelineDB.SaveResourceVersions(atc.ResourceConfig{
					Name:   "some-resource",
					Type:   "some-type",
					Source: atc.Source{"some": "source"},
				}, []atc.Version{{"version": "2"}})
				Expect(err).NotTo(HaveOccurred())

				savedVR2, found, err := pipelineDB.GetLatestVersionedResource(resource)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())

				err = pipelineDB.SaveResourceVersions(atc.ResourceConfig{
					Name:   "some-other-resource",
					Type:   "some-type",
					Source: atc.Source{"some": "source"},
				}, []atc.Version{{"version": "2"}})
				Expect(err).NotTo(HaveOccurred())

				otherSavedVR2, found, err := pipelineDB.GetLatestVersionedResource(otherResource)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())

				jobBuildInputs := []config.JobInput{
					{
						Name:     "some-input-name",
						Resource: "some-resource",
					},
					{
						Name:     "some-other-input-name",
						Resource: "some-other-resource",
					},
				}

				build1, err := pipelineDB.CreateJobBuild("some-job")
				Expect(err).NotTo(HaveOccurred())

				_, err = pipelineDB.SaveBuildInput(build1.ID, db.BuildInput{
					Name:              "some-input-name",
					VersionedResource: savedVR1.VersionedResource,
				})
				Expect(err).NotTo(HaveOccurred())

				_, err = pipelineDB.SaveBuildInput(build1.ID, db.BuildInput{
					Name:              "some-other-input-name",
					VersionedResource: otherSavedVR1.VersionedResource,
				})
				Expect(err).NotTo(HaveOccurred())

				build2, err := pipelineDB.CreateJobBuild("some-job")
				Expect(err).NotTo(HaveOccurred())

				_, err = pipelineDB.SaveBuildInput(build2.ID, db.BuildInput{
					Name:              "some-input-name",
					VersionedResource: savedVR2.VersionedResource,
				})

				Expect(err).NotTo(HaveOccurred())
				_, err = pipelineDB.SaveBuildInput(build2.ID, db.BuildInput{
					Name:              "some-other-input-name",
					VersionedResource: otherSavedVR2.VersionedResource,
				})
				Expect(err).NotTo(HaveOccurred())

				err = pipelineDB.DisableVersionedResource(savedVR2.ID)
				Expect(err).NotTo(HaveOccurred())

				versions, found, err := loadAndGetLatestInputVersions("some-job", jobBuildInputs)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())
				Expect(len(versions)).To(Equal(2))

				var someInput, someOtherInput db.BuildInput
				if versions[0].Name == "some-input-name" {
					someInput = versions[0]
					someOtherInput = versions[1]
				} else {
					someInput = versions[1]
					someOtherInput = versions[0]
				}

				Expect(someInput.Name).To(Equal("some-input-name"))
				Expect(someInput.VersionedResource.Resource).To(Equal(savedVR1.VersionedResource.Resource))
				Expect(someInput.VersionedResource.Type).To(Equal(savedVR1.VersionedResource.Type))
				Expect(someInput.VersionedResource.Version).To(Equal(savedVR1.VersionedResource.Version))
				Expect(someInput.VersionedResource.Metadata).To(Equal(savedVR1.VersionedResource.Metadata))
				Expect(someInput.VersionedResource.PipelineName).To(Equal(savedVR1.VersionedResource.PipelineName))

				Expect(someOtherInput.Name).To(Equal("some-other-input-name"))
				Expect(someOtherInput.VersionedResource.Resource).To(Equal(otherSavedVR2.VersionedResource.Resource))
				Expect(someOtherInput.VersionedResource.Type).To(Equal(savedVR2.VersionedResource.Type))
				Expect(someOtherInput.VersionedResource.Version).To(Equal(savedVR2.VersionedResource.Version))
				Expect(someOtherInput.VersionedResource.Metadata).To(Equal(savedVR2.VersionedResource.Metadata))
				Expect(someOtherInput.VersionedResource.PipelineName).To(Equal(savedVR2.VersionedResource.PipelineName))
			})

			It("prevents the resource version from being a candidate for build inputs", func() {
				err := pipelineDB.SaveResourceVersions(atc.ResourceConfig{
					Name:   "some-resource",
					Type:   "some-type",
					Source: atc.Source{"some": "source"},
				}, []atc.Version{{"version": "1"}})
				Expect(err).NotTo(HaveOccurred())

				savedVR1, found, err := pipelineDB.GetLatestVersionedResource(resource)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())

				err = pipelineDB.SaveResourceVersions(atc.ResourceConfig{
					Name:   "some-resource",
					Type:   "some-type",
					Source: atc.Source{"some": "source"},
				}, []atc.Version{{"version": "2"}})
				Expect(err).NotTo(HaveOccurred())

				savedVR2, found, err := pipelineDB.GetLatestVersionedResource(resource)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())

				jobBuildInputs := []config.JobInput{
					{
						Name:     "some-input-name",
						Resource: "some-resource",
					},
				}

				versions, found, err := loadAndGetLatestInputVersions("a-job", jobBuildInputs)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())

				someInput := versions[0]
				Expect(someInput.Name).To(Equal("some-input-name"))
				Expect(someInput.VersionedResource.Resource).To(Equal(savedVR2.VersionedResource.Resource))
				Expect(someInput.VersionedResource.Type).To(Equal(savedVR2.VersionedResource.Type))
				Expect(someInput.VersionedResource.Version).To(Equal(savedVR2.VersionedResource.Version))
				Expect(someInput.VersionedResource.Metadata).To(Equal(savedVR2.VersionedResource.Metadata))
				Expect(someInput.VersionedResource.PipelineName).To(Equal(savedVR2.VersionedResource.PipelineName))

				err = pipelineDB.DisableVersionedResource(savedVR2.ID)
				Expect(err).NotTo(HaveOccurred())

				versions, found, err = loadAndGetLatestInputVersions("a-job", jobBuildInputs)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())

				someInput = versions[0]
				Expect(someInput.Name).To(Equal("some-input-name"))
				Expect(someInput.VersionedResource.Resource).To(Equal(savedVR1.VersionedResource.Resource))
				Expect(someInput.VersionedResource.Type).To(Equal(savedVR1.VersionedResource.Type))
				Expect(someInput.VersionedResource.Version).To(Equal(savedVR1.VersionedResource.Version))
				Expect(someInput.VersionedResource.Metadata).To(Equal(savedVR1.VersionedResource.Metadata))
				Expect(someInput.VersionedResource.PipelineName).To(Equal(savedVR1.VersionedResource.PipelineName))

				err = pipelineDB.DisableVersionedResource(savedVR1.ID)
				Expect(err).NotTo(HaveOccurred())

				_, found, err = loadAndGetLatestInputVersions("a-job", jobBuildInputs)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeFalse())

				err = pipelineDB.EnableVersionedResource(savedVR1.ID)
				Expect(err).NotTo(HaveOccurred())

				versions, found, err = loadAndGetLatestInputVersions("a-job", jobBuildInputs)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())

				someInput = versions[0]
				Expect(someInput.Name).To(Equal("some-input-name"))
				Expect(someInput.VersionedResource.Resource).To(Equal(savedVR1.VersionedResource.Resource))
				Expect(someInput.VersionedResource.Type).To(Equal(savedVR1.VersionedResource.Type))
				Expect(someInput.VersionedResource.Version).To(Equal(savedVR1.VersionedResource.Version))
				Expect(someInput.VersionedResource.Metadata).To(Equal(savedVR1.VersionedResource.Metadata))
				Expect(someInput.VersionedResource.PipelineName).To(Equal(savedVR1.VersionedResource.PipelineName))

				err = pipelineDB.EnableVersionedResource(savedVR2.ID)
				Expect(err).NotTo(HaveOccurred())

				versions, found, err = loadAndGetLatestInputVersions("a-job", jobBuildInputs)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())

				someInput = versions[0]
				Expect(someInput.Name).To(Equal("some-input-name"))
				Expect(someInput.VersionedResource.Resource).To(Equal(savedVR2.VersionedResource.Resource))
				Expect(someInput.VersionedResource.Type).To(Equal(savedVR2.VersionedResource.Type))
				Expect(someInput.VersionedResource.Version).To(Equal(savedVR2.VersionedResource.Version))
				Expect(someInput.VersionedResource.Metadata).To(Equal(savedVR2.VersionedResource.Metadata))
				Expect(someInput.VersionedResource.PipelineName).To(Equal(savedVR2.VersionedResource.PipelineName))
			})
		})

		Describe("VersionsDB caching", func() {
			Context("when build outputs are added", func() {
				var build db.Build
				var savedVR db.SavedVersionedResource

				BeforeEach(func() {
					var err error
					build, err = pipelineDB.CreateJobBuild("some-job")
					Expect(err).NotTo(HaveOccurred())

					err = pipelineDB.SaveResourceVersions(atc.ResourceConfig{
						Name:   "some-resource",
						Type:   "some-type",
						Source: atc.Source{"some": "source"},
					}, []atc.Version{{"version": "1"}})
					Expect(err).NotTo(HaveOccurred())

					savedResource, err := pipelineDB.GetResource("some-resource")
					Expect(err).NotTo(HaveOccurred())

					var found bool
					savedVR, found, err = pipelineDB.GetLatestVersionedResource(savedResource)
					Expect(err).NotTo(HaveOccurred())
					Expect(found).To(BeTrue())
				})

				It("will cache VersionsDB if no change has occured", func() {
					_, err := pipelineDB.SaveBuildOutput(build.ID, savedVR.VersionedResource, true)

					versionsDB, err := pipelineDB.LoadVersionsDB()
					Expect(err).NotTo(HaveOccurred())

					cachedVersionsDB, err := pipelineDB.LoadVersionsDB()
					Expect(err).NotTo(HaveOccurred())
					Expect(versionsDB == cachedVersionsDB).To(BeTrue(), "Expected VersionsDB to be the same object")
				})

				It("will not cache VersionsDB if a change occured", func() {
					versionsDB, err := pipelineDB.LoadVersionsDB()
					Expect(err).NotTo(HaveOccurred())

					_, err = pipelineDB.SaveBuildOutput(build.ID, savedVR.VersionedResource, true)
					Expect(err).NotTo(HaveOccurred())

					cachedVersionsDB, err := pipelineDB.LoadVersionsDB()
					Expect(err).NotTo(HaveOccurred())
					Expect(versionsDB != cachedVersionsDB).To(BeTrue(), "Expected VersionsDB to be different objects")
				})

				Context("when the build outputs are added for a different pipeline", func() {
					It("does not invalidate the cache for the original pipeline", func() {
						otherBuild, err := otherPipelineDB.CreateJobBuild("some-job")
						Expect(err).NotTo(HaveOccurred())

						err = otherPipelineDB.SaveResourceVersions(atc.ResourceConfig{
							Name:   "some-other-resource",
							Type:   "some-type",
							Source: atc.Source{"some": "source"},
						}, []atc.Version{{"version": "1"}})
						Expect(err).NotTo(HaveOccurred())

						otherSavedResource, err := otherPipelineDB.GetResource("some-other-resource")
						Expect(err).NotTo(HaveOccurred())

						otherSavedVR, found, err := otherPipelineDB.GetLatestVersionedResource(otherSavedResource)
						Expect(err).NotTo(HaveOccurred())
						Expect(found).To(BeTrue())

						versionsDB, err := pipelineDB.LoadVersionsDB()
						Expect(err).NotTo(HaveOccurred())

						_, err = otherPipelineDB.SaveBuildOutput(otherBuild.ID, otherSavedVR.VersionedResource, true)
						Expect(err).NotTo(HaveOccurred())

						cachedVersionsDB, err := pipelineDB.LoadVersionsDB()
						Expect(err).NotTo(HaveOccurred())
						Expect(versionsDB == cachedVersionsDB).To(BeTrue(), "Expected VersionsDB to be the same object")
					})
				})
			})

			Context("when versioned resources are added", func() {
				It("will cache VersionsDB if no change has occured", func() {
					err := pipelineDB.SaveResourceVersions(atc.ResourceConfig{
						Name:   "some-resource",
						Type:   "some-type",
						Source: atc.Source{"some": "source"},
					}, []atc.Version{{"version": "1"}})
					Expect(err).NotTo(HaveOccurred())

					versionsDB, err := pipelineDB.LoadVersionsDB()
					Expect(err).NotTo(HaveOccurred())

					cachedVersionsDB, err := pipelineDB.LoadVersionsDB()
					Expect(err).NotTo(HaveOccurred())
					Expect(versionsDB == cachedVersionsDB).To(BeTrue(), "Expected VersionsDB to be the same object")
				})

				It("will not cache VersionsDB if a change occured", func() {
					err := pipelineDB.SaveResourceVersions(atc.ResourceConfig{
						Name:   "some-resource",
						Type:   "some-type",
						Source: atc.Source{"some": "source"},
					}, []atc.Version{{"version": "1"}})
					Expect(err).NotTo(HaveOccurred())

					versionsDB, err := pipelineDB.LoadVersionsDB()
					Expect(err).NotTo(HaveOccurred())

					err = pipelineDB.SaveResourceVersions(atc.ResourceConfig{
						Name:   "some-other-resource",
						Type:   "some-type",
						Source: atc.Source{"some": "source"},
					}, []atc.Version{{"version": "1"}})
					Expect(err).NotTo(HaveOccurred())

					cachedVersionsDB, err := pipelineDB.LoadVersionsDB()
					Expect(err).NotTo(HaveOccurred())
					Expect(versionsDB != cachedVersionsDB).To(BeTrue(), "Expected VersionsDB to be different objects")
				})

				Context("when the versioned resources are added for a different pipeline", func() {
					It("does not invalidate the cache for the original pipeline", func() {
						err := pipelineDB.SaveResourceVersions(atc.ResourceConfig{
							Name:   "some-resource",
							Type:   "some-type",
							Source: atc.Source{"some": "source"},
						}, []atc.Version{{"version": "1"}})
						Expect(err).NotTo(HaveOccurred())

						versionsDB, err := pipelineDB.LoadVersionsDB()
						Expect(err).NotTo(HaveOccurred())

						err = otherPipelineDB.SaveResourceVersions(atc.ResourceConfig{
							Name:   "some-other-resource",
							Type:   "some-type",
							Source: atc.Source{"some": "source"},
						}, []atc.Version{{"version": "1"}})
						Expect(err).NotTo(HaveOccurred())

						cachedVersionsDB, err := pipelineDB.LoadVersionsDB()
						Expect(err).NotTo(HaveOccurred())
						Expect(versionsDB == cachedVersionsDB).To(BeTrue(), "Expected VersionsDB to be the same object")
					})
				})
			})
		})

		Describe("saving versioned resources", func() {
			It("updates the latest versioned resource", func() {
				err := pipelineDB.SaveResourceVersions(
					atc.ResourceConfig{
						Name:   "some-resource",
						Type:   "some-type",
						Source: atc.Source{"some": "source"},
					},
					[]atc.Version{{"version": "1"}},
				)
				Expect(err).NotTo(HaveOccurred())

				savedResource, err := pipelineDB.GetResource("some-resource")
				Expect(err).NotTo(HaveOccurred())

				savedVR, found, err := pipelineDB.GetLatestVersionedResource(savedResource)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())

				Expect(savedVR.Resource).To(Equal("some-resource"))
				Expect(savedVR.Type).To(Equal("some-type"))
				Expect(savedVR.Version).To(Equal(db.Version{"version": "1"}))

				err = pipelineDB.SaveResourceVersions(atc.ResourceConfig{
					Name:   "some-resource",
					Type:   "some-type",
					Source: atc.Source{"some": "source"},
				}, []atc.Version{{"version": "2"}, {"version": "3"}})
				Expect(err).NotTo(HaveOccurred())

				savedVR, found, err = pipelineDB.GetLatestVersionedResource(savedResource)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())

				Expect(savedVR.Resource).To(Equal("some-resource"))
				Expect(savedVR.Type).To(Equal("some-type"))
				Expect(savedVR.Version).To(Equal(db.Version{"version": "3"}))
			})
		})

		It("initially reports zero builds for a job", func() {
			builds, err := pipelineDB.GetAllJobBuilds("some-job")
			Expect(err).NotTo(HaveOccurred())
			Expect(builds).To(BeEmpty())
		})

		It("initially has no current build for a job", func() {
			_, found, err := pipelineDB.GetCurrentBuild("some-job")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeFalse())
		})

		It("initially has no pending build for a job", func() {
			_, found, err := pipelineDB.GetNextPendingBuild("some-job")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeFalse())
		})

		Describe("marking resource checks as errored", func() {
			var resource db.SavedResource

			BeforeEach(func() {
				var err error
				resource, err = pipelineDB.GetResource("some-resource")
				Expect(err).NotTo(HaveOccurred())
			})

			Context("when the resource is first created", func() {
				It("is not errored", func() {
					Expect(resource.CheckError).To(BeNil())
				})
			})

			Context("when a resource check is marked as errored", func() {
				It("is then marked as errored", func() {
					originalCause := errors.New("on fire")

					err := pipelineDB.SetResourceCheckError(resource, originalCause)
					Expect(err).NotTo(HaveOccurred())

					returnedResource, err := pipelineDB.GetResource("some-resource")
					Expect(err).NotTo(HaveOccurred())

					Expect(returnedResource.CheckError).To(Equal(originalCause))
				})
			})

			Context("when a resource is cleared of check errors", func() {
				It("is not marked as errored again", func() {
					originalCause := errors.New("on fire")

					err := pipelineDB.SetResourceCheckError(resource, originalCause)
					Expect(err).NotTo(HaveOccurred())

					err = pipelineDB.SetResourceCheckError(resource, nil)
					Expect(err).NotTo(HaveOccurred())

					returnedResource, err := pipelineDB.GetResource("some-resource")
					Expect(err).NotTo(HaveOccurred())

					Expect(returnedResource.CheckError).To(BeNil())
				})
			})
		})
	})

	Describe("Jobs", func() {
		Describe("GetDashboard", func() {
			It("returns a Dashboard object with a DashboardJob corresponding to each configured job", func() {
				job, err := pipelineDB.GetJob("some-job")
				Expect(err).NotTo(HaveOccurred())

				otherJob, err := pipelineDB.GetJob("some-other-job")
				Expect(err).NotTo(HaveOccurred())

				aJob, err := pipelineDB.GetJob("a-job")
				Expect(err).NotTo(HaveOccurred())

				sharedJob, err := pipelineDB.GetJob("shared-job")
				Expect(err).NotTo(HaveOccurred())

				randomJob, err := pipelineDB.GetJob("random-job")
				Expect(err).NotTo(HaveOccurred())

				otherSerialGroupJob, err := pipelineDB.GetJob("other-serial-group-job")
				Expect(err).NotTo(HaveOccurred())

				differentSerialGroupJob, err := pipelineDB.GetJob("different-serial-group-job")
				Expect(err).NotTo(HaveOccurred())

				lookupConfig := func(jobName string) atc.JobConfig {
					config, found := pipelineConfig.Jobs.Lookup(jobName)
					Expect(found).To(BeTrue())

					return config
				}

				By("returning jobs with no builds")
				expectedDashboard := db.Dashboard{
					{
						JobConfig:     lookupConfig(job.Name),
						Job:           job,
						NextBuild:     nil,
						FinishedBuild: nil,
					},
					{
						JobConfig:     lookupConfig(otherJob.Name),
						Job:           otherJob,
						NextBuild:     nil,
						FinishedBuild: nil,
					},
					{
						JobConfig:     lookupConfig(aJob.Name),
						Job:           aJob,
						NextBuild:     nil,
						FinishedBuild: nil,
					},
					{
						JobConfig:     lookupConfig(sharedJob.Name),
						Job:           sharedJob,
						NextBuild:     nil,
						FinishedBuild: nil,
					},
					{
						JobConfig:     lookupConfig(randomJob.Name),
						Job:           randomJob,
						NextBuild:     nil,
						FinishedBuild: nil,
					},
					{
						JobConfig:     lookupConfig(otherSerialGroupJob.Name),
						Job:           otherSerialGroupJob,
						NextBuild:     nil,
						FinishedBuild: nil,
					},
					{
						JobConfig:     lookupConfig(differentSerialGroupJob.Name),
						Job:           differentSerialGroupJob,
						NextBuild:     nil,
						FinishedBuild: nil,
					},
				}

				actualDashboard, groups, err := pipelineDB.GetDashboard()
				Expect(err).NotTo(HaveOccurred())

				Expect(groups).To(Equal(pipelineConfig.Groups))
				Expect(actualDashboard).To(ConsistOf(expectedDashboard))

				By("returning a job's most recent pending build if there are no running builds")
				jobBuildOld, err := pipelineDB.CreateJobBuild("some-job")
				Expect(err).NotTo(HaveOccurred())

				expectedDashboard[0].NextBuild = &jobBuildOld

				actualDashboard, _, err = pipelineDB.GetDashboard()
				Expect(err).NotTo(HaveOccurred())

				Expect(actualDashboard).To(ConsistOf(expectedDashboard))

				By("returning a job's most recent started build")
				sqlDB.StartBuild(jobBuildOld.ID, "engine", "metadata")

				var found bool
				jobBuildOld, found, err = sqlDB.GetBuild(jobBuildOld.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())

				expectedDashboard[0].NextBuild = &jobBuildOld

				actualDashboard, _, err = pipelineDB.GetDashboard()
				Expect(err).NotTo(HaveOccurred())

				Expect(actualDashboard).To(ConsistOf(expectedDashboard))

				By("returning a job's most recent started build even if there is a newer pending build")
				jobBuild, err := pipelineDB.CreateJobBuild("some-job")
				Expect(err).NotTo(HaveOccurred())

				expectedDashboard[0].NextBuild = &jobBuildOld

				actualDashboard, _, err = pipelineDB.GetDashboard()
				Expect(err).NotTo(HaveOccurred())

				Expect(actualDashboard).To(ConsistOf(expectedDashboard))

				By("returning a job's most recent finished build")
				err = sqlDB.FinishBuild(jobBuild.ID, db.StatusSucceeded)
				Expect(err).NotTo(HaveOccurred())

				jobBuild, found, err = sqlDB.GetBuild(jobBuild.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())

				expectedDashboard[0].FinishedBuild = &jobBuild
				expectedDashboard[0].NextBuild = &jobBuildOld

				actualDashboard, _, err = pipelineDB.GetDashboard()
				Expect(err).NotTo(HaveOccurred())

				Expect(actualDashboard).To(ConsistOf(expectedDashboard))

				By("returning a job's most recent finished build even when there is a newer unfinished build")
				jobBuildNew, err := pipelineDB.CreateJobBuild("some-job")
				Expect(err).NotTo(HaveOccurred())
				sqlDB.StartBuild(jobBuildNew.ID, "engine", "metadata")
				jobBuildNew, found, err = sqlDB.GetBuild(jobBuildNew.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())

				expectedDashboard[0].FinishedBuild = &jobBuild
				expectedDashboard[0].NextBuild = &jobBuildNew

				actualDashboard, _, err = pipelineDB.GetDashboard()
				Expect(err).NotTo(HaveOccurred())

				Expect(actualDashboard).To(ConsistOf(expectedDashboard))
			})
		})

		Describe("CreateJobBuild", func() {
			var build db.Build

			BeforeEach(func() {
				var err error
				build, err = pipelineDB.CreateJobBuild("some-job")
				Expect(err).NotTo(HaveOccurred())
			})

			It("sets the properties of a build for a given job", func() {
				Expect(build.ID).NotTo(BeZero())
				Expect(build.JobID).NotTo(BeZero())
				Expect(build.Name).To(Equal("1"))
				Expect(build.Status).To(Equal(db.StatusPending))
				Expect(build.Scheduled).To(BeFalse())
			})

			It("creates an entry in build_preparation", func() {
				buildPrep, found, err := sqlDB.GetBuildPreparation(build.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())

				Expect(buildPrep.BuildID).To(Equal(build.ID))
			})
		})

		Describe("saving builds for scheduling", func() {
			buildMetadata := []db.MetadataField{
				{
					Name:  "meta1",
					Value: "value1",
				},
				{
					Name:  "meta2",
					Value: "value2",
				},
			}

			vr1 := db.VersionedResource{
				PipelineName: "a-pipeline-name",
				Resource:     "some-resource",
				Type:         "some-type",
				Version:      db.Version{"ver": "1"},
				Metadata:     buildMetadata,
			}

			vr2 := db.VersionedResource{
				PipelineName: "a-pipeline-name",
				Resource:     "some-other-resource",
				Type:         "some-type",
				Version:      db.Version{"ver": "2"},
			}

			input1 := db.BuildInput{
				Name:              "some-input",
				VersionedResource: vr1,
			}

			input2 := db.BuildInput{
				Name:              "some-other-input",
				VersionedResource: vr2,
			}

			inputs := []db.BuildInput{input1, input2}

			It("does not create a new build if one is already running that does not have determined inputs ", func() {
				build, created, err := pipelineDB.CreateJobBuildForCandidateInputs("some-job")
				Expect(err).NotTo(HaveOccurred())
				Expect(created).To(BeTrue())

				Expect(build.ID).NotTo(BeZero())
				Expect(build.JobID).NotTo(BeZero())
				Expect(build.Name).To(Equal("1"))
				Expect(build.Status).To(Equal(db.StatusPending))
				Expect(build.Scheduled).To(BeFalse())

				_, created, err = pipelineDB.CreateJobBuildForCandidateInputs("some-job")
				Expect(err).NotTo(HaveOccurred())
				Expect(created).To(BeFalse())
			})

			It("does create a new build if one does not have determined inputs but it has a different name", func() {
				_, created, err := pipelineDB.CreateJobBuildForCandidateInputs("some-job")
				Expect(err).NotTo(HaveOccurred())
				Expect(created).To(BeTrue())

				_, created, err = pipelineDB.CreateJobBuildForCandidateInputs("some-other-job")
				Expect(err).NotTo(HaveOccurred())
				Expect(created).To(BeTrue())
			})

			It("does create a new build if one does not have determined inputs but in a different pipeline", func() {
				_, err := otherPipelineDB.CreateJobBuild("some-job")
				Expect(err).NotTo(HaveOccurred())

				_, created, err := pipelineDB.CreateJobBuildForCandidateInputs("some-job")
				Expect(err).NotTo(HaveOccurred())
				Expect(created).To(BeTrue())
			})

			It("does create a new build if one is already saved but it has already locked down its inputs", func() {
				build, created, err := pipelineDB.CreateJobBuildForCandidateInputs("some-job")
				Expect(err).NotTo(HaveOccurred())
				Expect(created).To(BeTrue())

				err = pipelineDB.UseInputsForBuild(build.ID, inputs)
				Expect(err).NotTo(HaveOccurred())

				_, created, err = pipelineDB.CreateJobBuildForCandidateInputs("some-job")
				Expect(err).NotTo(HaveOccurred())
				Expect(created).To(BeTrue())
			})

			It("does create a new build if one is already saved but does not have determined inputs but is not running (errored)", func() {
				build, created, err := pipelineDB.CreateJobBuildForCandidateInputs("some-job")
				Expect(err).NotTo(HaveOccurred())
				Expect(created).To(BeTrue())

				err = sqlDB.ErrorBuild(build.ID, errors.New("disaster"))
				Expect(err).NotTo(HaveOccurred())

				_, created, err = pipelineDB.CreateJobBuildForCandidateInputs("some-job")
				Expect(err).NotTo(HaveOccurred())
				Expect(created).To(BeTrue())
			})

			It("does create a new build if one is already saved but does not have determined inputs but is not running (aborted)", func() {
				build, created, err := pipelineDB.CreateJobBuildForCandidateInputs("some-job")
				Expect(err).NotTo(HaveOccurred())
				Expect(created).To(BeTrue())

				err = sqlDB.AbortBuild(build.ID)
				Expect(err).NotTo(HaveOccurred())

				_, created, err = pipelineDB.CreateJobBuildForCandidateInputs("some-job")
				Expect(err).NotTo(HaveOccurred())
				Expect(created).To(BeTrue())
			})

			It("does create a new build if one is already saved but does not have determined inputs but is not running (succeeded)", func() {
				build, created, err := pipelineDB.CreateJobBuildForCandidateInputs("some-job")
				Expect(err).NotTo(HaveOccurred())
				Expect(created).To(BeTrue())

				err = sqlDB.FinishBuild(build.ID, db.StatusSucceeded)
				Expect(err).NotTo(HaveOccurred())

				_, created, err = pipelineDB.CreateJobBuildForCandidateInputs("some-job")
				Expect(err).NotTo(HaveOccurred())
				Expect(created).To(BeTrue())
			})

			It("does create a new build if one is already saved but does not have determined inputs but is not running (failed)", func() {
				build, created, err := pipelineDB.CreateJobBuildForCandidateInputs("some-job")
				Expect(err).NotTo(HaveOccurred())
				Expect(created).To(BeTrue())

				err = sqlDB.FinishBuild(build.ID, db.StatusFailed)
				Expect(err).NotTo(HaveOccurred())

				_, created, err = pipelineDB.CreateJobBuildForCandidateInputs("some-job")
				Expect(err).NotTo(HaveOccurred())
				Expect(created).To(BeTrue())
			})

			It("saves all the build inputs", func() {
				build, created, err := pipelineDB.CreateJobBuildForCandidateInputs("some-job")
				Expect(err).NotTo(HaveOccurred())
				Expect(created).To(BeTrue())
				expectedBuild := build
				expectedBuild.InputsDetermined = true

				err = pipelineDB.UseInputsForBuild(build.ID, inputs)
				Expect(err).NotTo(HaveOccurred())

				foundBuild, found, err := pipelineDB.GetJobBuildForInputs("some-job", []db.BuildInput{
					input1,
					input2,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())
				Expect(foundBuild).To(Equal(expectedBuild))
			})

			It("removes old build inputs", func() {
				vr3 := db.VersionedResource{
					PipelineName: "a-pipeline-name",
					Resource:     "some-really-other-resource",
					Type:         "some-type",
					Version:      db.Version{"ver": "3"},
				}
				input3 := db.BuildInput{
					Name:              "some-really-other-input",
					VersionedResource: vr3,
				}

				build, created, err := pipelineDB.CreateJobBuildForCandidateInputs("some-job")
				Expect(err).NotTo(HaveOccurred())
				Expect(created).To(BeTrue())
				expectedBuild := build
				expectedBuild.InputsDetermined = true

				err = pipelineDB.UseInputsForBuild(build.ID, inputs)
				Expect(err).NotTo(HaveOccurred())

				updatedInputs := []db.BuildInput{input3}
				err = pipelineDB.UseInputsForBuild(build.ID, updatedInputs)
				Expect(err).NotTo(HaveOccurred())

				_, found, err := pipelineDB.GetJobBuildForInputs("some-job", []db.BuildInput{
					input1,
					input2,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeFalse())

				foundBuild, found, err := pipelineDB.GetJobBuildForInputs("some-job", []db.BuildInput{
					input3,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())
				Expect(foundBuild).To(Equal(expectedBuild))
			})

			It("creates an entry in build_preparation", func() {
				build, created, err := pipelineDB.CreateJobBuildForCandidateInputs("some-job")
				Expect(err).NotTo(HaveOccurred())
				Expect(created).To(BeTrue())

				buildPrep, found, err := sqlDB.GetBuildPreparation(build.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())

				Expect(buildPrep.BuildID).To(Equal(build.ID))
			})
		})

		Describe("saving build inputs", func() {
			buildMetadata := []db.MetadataField{
				{
					Name:  "meta1",
					Value: "value1",
				},
				{
					Name:  "meta2",
					Value: "value2",
				},
			}

			vr1 := db.VersionedResource{
				PipelineName: "a-pipeline-name",
				Resource:     "some-resource",
				Type:         "some-type",
				Version:      db.Version{"ver": "1"},
				Metadata:     buildMetadata,
			}

			vr2 := db.VersionedResource{
				PipelineName: "a-pipeline-name",
				Resource:     "some-other-resource",
				Type:         "some-type",
				Version:      db.Version{"ver": "2"},
			}

			It("saves build's inputs and outputs as versioned resources", func() {
				build, err := pipelineDB.CreateJobBuild("some-job")
				Expect(err).NotTo(HaveOccurred())

				input1 := db.BuildInput{
					Name:              "some-input",
					VersionedResource: vr1,
				}

				input2 := db.BuildInput{
					Name:              "some-other-input",
					VersionedResource: vr2,
				}

				otherInput := db.BuildInput{
					Name:              "some-random-input",
					VersionedResource: vr2,
				}

				_, err = sqlDB.SaveBuildInput(team.Name, build.ID, input1)
				Expect(err).NotTo(HaveOccurred())

				_, found, err := pipelineDB.GetJobBuildForInputs("some-job", []db.BuildInput{
					input1,
					input2,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeFalse())

				_, err = sqlDB.SaveBuildInput(team.Name, build.ID, otherInput)
				Expect(err).NotTo(HaveOccurred())

				_, found, err = pipelineDB.GetJobBuildForInputs("some-job", []db.BuildInput{
					input1,
					input2,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeFalse())

				_, err = sqlDB.SaveBuildInput(team.Name, build.ID, input2)
				Expect(err).NotTo(HaveOccurred())

				foundBuild, found, err := pipelineDB.GetJobBuildForInputs("some-job", []db.BuildInput{
					input1,
					input2,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())
				Expect(foundBuild).To(Equal(build))

				modifiedVR2 := vr2
				modifiedVR2.Version = db.Version{"ver": "3"}

				inputs, _, err := sqlDB.GetBuildResources(build.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(inputs).To(ConsistOf([]db.BuildInput{
					{Name: "some-input", VersionedResource: vr1, FirstOccurrence: true},
					{Name: "some-other-input", VersionedResource: vr2, FirstOccurrence: true},
					{Name: "some-random-input", VersionedResource: vr2, FirstOccurrence: true},
				}))

				duplicateBuild, err := pipelineDB.CreateJobBuild("some-job")
				Expect(err).NotTo(HaveOccurred())

				_, err = sqlDB.SaveBuildInput(team.Name, duplicateBuild.ID, db.BuildInput{
					Name:              "other-build-input",
					VersionedResource: vr1,
				})
				Expect(err).NotTo(HaveOccurred())

				_, err = sqlDB.SaveBuildInput(team.Name, duplicateBuild.ID, db.BuildInput{
					Name:              "other-build-other-input",
					VersionedResource: vr2,
				})
				Expect(err).NotTo(HaveOccurred())

				inputs, _, err = sqlDB.GetBuildResources(duplicateBuild.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(inputs).To(ConsistOf([]db.BuildInput{
					{Name: "other-build-input", VersionedResource: vr1, FirstOccurrence: false},
					{Name: "other-build-other-input", VersionedResource: vr2, FirstOccurrence: false},
				}))

				newBuildInOtherJob, err := pipelineDB.CreateJobBuild("some-other-job")
				Expect(err).NotTo(HaveOccurred())

				_, err = sqlDB.SaveBuildInput(team.Name, newBuildInOtherJob.ID, db.BuildInput{
					Name:              "other-job-input",
					VersionedResource: vr1,
				})
				Expect(err).NotTo(HaveOccurred())

				_, err = sqlDB.SaveBuildInput(team.Name, newBuildInOtherJob.ID, db.BuildInput{
					Name:              "other-job-other-input",
					VersionedResource: vr2,
				})
				Expect(err).NotTo(HaveOccurred())

				inputs, _, err = sqlDB.GetBuildResources(newBuildInOtherJob.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(inputs).To(ConsistOf([]db.BuildInput{
					{Name: "other-job-input", VersionedResource: vr1, FirstOccurrence: true},
					{Name: "other-job-other-input", VersionedResource: vr2, FirstOccurrence: true},
				}))

			})

			It("updates metadata of existing versioned resources", func() {
				build, err := pipelineDB.CreateJobBuild("some-job")
				Expect(err).NotTo(HaveOccurred())

				_, err = sqlDB.SaveBuildInput(team.Name, build.ID, db.BuildInput{
					Name:              "some-input",
					VersionedResource: vr2,
				})
				Expect(err).NotTo(HaveOccurred())

				inputs, _, err := sqlDB.GetBuildResources(build.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(inputs).To(ConsistOf([]db.BuildInput{
					{Name: "some-input", VersionedResource: vr2, FirstOccurrence: true},
				}))

				withMetadata := vr2
				withMetadata.Metadata = buildMetadata

				_, err = sqlDB.SaveBuildInput(team.Name, build.ID, db.BuildInput{
					Name:              "some-other-input",
					VersionedResource: withMetadata,
				})
				Expect(err).NotTo(HaveOccurred())

				inputs, _, err = sqlDB.GetBuildResources(build.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(inputs).To(ConsistOf([]db.BuildInput{
					{Name: "some-input", VersionedResource: withMetadata, FirstOccurrence: true},
					{Name: "some-other-input", VersionedResource: withMetadata, FirstOccurrence: true},
				}))

				_, err = sqlDB.SaveBuildInput(team.Name, build.ID, db.BuildInput{
					Name:              "some-input",
					VersionedResource: withMetadata,
				})
				Expect(err).NotTo(HaveOccurred())

				inputs, _, err = sqlDB.GetBuildResources(build.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(inputs).To(ConsistOf([]db.BuildInput{
					{Name: "some-input", VersionedResource: withMetadata, FirstOccurrence: true},
					{Name: "some-other-input", VersionedResource: withMetadata, FirstOccurrence: true},
				}))

			})

			It("does not clobber metadata of existing versioned resources", func() {
				build, err := pipelineDB.CreateJobBuild("some-job")
				Expect(err).NotTo(HaveOccurred())

				withMetadata := vr2
				withMetadata.Metadata = buildMetadata

				withoutMetadata := vr2
				withoutMetadata.Metadata = nil

				savedVR, err := sqlDB.SaveBuildInput(team.Name, build.ID, db.BuildInput{
					Name:              "some-input",
					VersionedResource: withMetadata,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(savedVR.Metadata).To(Equal(buildMetadata))

				inputs, _, err := sqlDB.GetBuildResources(build.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(inputs).To(ConsistOf([]db.BuildInput{
					{Name: "some-input", VersionedResource: withMetadata, FirstOccurrence: true},
				}))

				savedVR, err = sqlDB.SaveBuildInput(team.Name, build.ID, db.BuildInput{
					Name:              "some-other-input",
					VersionedResource: withoutMetadata,
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(savedVR.Metadata).To(Equal(buildMetadata))

				inputs, _, err = sqlDB.GetBuildResources(build.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(inputs).To(ConsistOf([]db.BuildInput{
					{Name: "some-input", VersionedResource: withMetadata, FirstOccurrence: true},
					{Name: "some-other-input", VersionedResource: withMetadata, FirstOccurrence: true},
				}))

			})
		})

		Describe("saving inputs, implicit outputs, and explicit outputs", func() {
			vr1 := db.VersionedResource{
				PipelineName: "a-pipeline-name",
				Resource:     "some-resource",
				Type:         "some-type",
				Version:      db.Version{"ver": "1"},
			}

			vr2 := db.VersionedResource{
				PipelineName: "a-pipeline-name",
				Resource:     "some-other-resource",
				Type:         "some-type",
				Version:      db.Version{"ver": "2"},
			}

			It("correctly distinguishes them", func() {
				build, err := pipelineDB.CreateJobBuild("some-job")
				Expect(err).NotTo(HaveOccurred())

				// save a normal 'get'
				_, err = sqlDB.SaveBuildInput(team.Name, build.ID, db.BuildInput{
					Name:              "some-input",
					VersionedResource: vr1,
				})
				Expect(err).NotTo(HaveOccurred())

				// save implicit output from 'get'
				_, err = sqlDB.SaveBuildOutput(team.Name, build.ID, vr1, false)
				Expect(err).NotTo(HaveOccurred())

				// save explicit output from 'put'
				_, err = sqlDB.SaveBuildOutput(team.Name, build.ID, vr2, true)
				Expect(err).NotTo(HaveOccurred())

				// save the dependent get
				_, err = sqlDB.SaveBuildInput(team.Name, build.ID, db.BuildInput{
					Name:              "some-dependent-input",
					VersionedResource: vr2,
				})
				Expect(err).NotTo(HaveOccurred())

				// save the dependent 'get's implicit output
				_, err = sqlDB.SaveBuildOutput(team.Name, build.ID, vr2, false)
				Expect(err).NotTo(HaveOccurred())

				inputs, outputs, err := sqlDB.GetBuildResources(build.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(inputs).To(ConsistOf([]db.BuildInput{
					{Name: "some-input", VersionedResource: vr1, FirstOccurrence: true},
				}))

				Expect(outputs).To(ConsistOf([]db.BuildOutput{
					{VersionedResource: vr2},
				}))

			})
		})

		Describe("pausing and unpausing jobs", func() {
			job := "some-job"

			It("starts out as unpaused", func() {
				job, err := pipelineDB.GetJob(job)
				Expect(err).NotTo(HaveOccurred())

				Expect(job.Paused).To(BeFalse())
			})

			It("can be paused", func() {
				err := pipelineDB.PauseJob(job)
				Expect(err).NotTo(HaveOccurred())

				err = otherPipelineDB.UnpauseJob(job)
				Expect(err).NotTo(HaveOccurred())

				pausedJob, err := pipelineDB.GetJob(job)
				Expect(err).NotTo(HaveOccurred())
				Expect(pausedJob.Paused).To(BeTrue())

				otherJob, err := otherPipelineDB.GetJob(job)
				Expect(err).NotTo(HaveOccurred())
				Expect(otherJob.Paused).To(BeFalse())
			})

			It("can be unpaused", func() {
				err := pipelineDB.PauseJob(job)
				Expect(err).NotTo(HaveOccurred())

				err = pipelineDB.UnpauseJob(job)
				Expect(err).NotTo(HaveOccurred())

				unpausedJob, err := pipelineDB.GetJob(job)
				Expect(err).NotTo(HaveOccurred())

				Expect(unpausedJob.Paused).To(BeFalse())
			})
		})

		Describe("GetJobBuild", func() {
			var firstBuild db.Build
			var job db.SavedJob

			BeforeEach(func() {
				var err error
				job, err = pipelineDB.GetJob("some-job")
				Expect(err).NotTo(HaveOccurred())

				firstBuild, err = pipelineDB.CreateJobBuild(job.Name)
				Expect(err).NotTo(HaveOccurred())
			})

			It("finds the build", func() {
				build, found, err := pipelineDB.GetJobBuild(job.Name, firstBuild.Name)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())
				Expect(build.ID).To(Equal(firstBuild.ID))
				Expect(build.Status).To(Equal(firstBuild.Status))
			})
		})

		Describe("GetNextPendingBuildBySerialGroup", func() {
			var jobOneConfig atc.JobConfig
			var jobOneTwoConfig atc.JobConfig

			BeforeEach(func() {
				jobOneConfig = atc.JobConfig{
					Name:         "some-job",
					SerialGroups: []string{"one"},
				}
				jobOneTwoConfig = atc.JobConfig{
					Name:         "some-other-job",
					SerialGroups: []string{"one", "two"},
				}
			})

			Context("when some jobs have builds with inputs determined as false", func() {
				var acutalBuild db.Build

				BeforeEach(func() {
					//TODO: Delete this query after #114257887
					_, err := dbConn.Query(`
				INSERT INTO jobs_serial_groups (serial_group, job_id) VALUES
				('one', (select j.id from jobs j, pipelines p where j.name = 'some-job' and j.pipeline_id = p.id and p.name = $1)),
				('one', (select j.id from jobs j, pipelines p where j.name = 'some-other-job' and j.pipeline_id = p.id and p.name = $1)),
				('two', (select j.id from jobs j, pipelines p where j.name = 'some-other-job' and j.pipeline_id = p.id and p.name = $1))
			`, pipelineDB.GetPipelineName())
					Expect(err).NotTo(HaveOccurred())
				})

				BeforeEach(func() {
					_, err := pipelineDB.CreateJobBuild(jobOneConfig.Name)
					Expect(err).NotTo(HaveOccurred())

					acutalBuild, err = pipelineDB.CreateJobBuild(jobOneTwoConfig.Name)
					Expect(err).NotTo(HaveOccurred())
					_, err = dbConn.Query(`
						UPDATE builds
						SET inputs_determined = true
						WHERE id = $1
					`, acutalBuild.ID)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should return the next most pending build in a group of jobs", func() {
					build, found, err := pipelineDB.GetNextPendingBuildBySerialGroup(jobOneConfig.Name, []string{"one"})
					Expect(err).NotTo(HaveOccurred())
					Expect(found).To(BeTrue())
					Expect(build.ID).To(Equal(acutalBuild.ID))
				})
			})

			It("should return the next most pending build in a group of jobs", func() {
				buildOne, err := pipelineDB.CreateJobBuild(jobOneConfig.Name)
				Expect(err).NotTo(HaveOccurred())

				buildTwo, err := pipelineDB.CreateJobBuild(jobOneConfig.Name)
				Expect(err).NotTo(HaveOccurred())

				buildThree, err := pipelineDB.CreateJobBuild(jobOneTwoConfig.Name)
				Expect(err).NotTo(HaveOccurred())

				otherBuildOne, err := otherPipelineDB.CreateJobBuild(jobOneConfig.Name)
				Expect(err).NotTo(HaveOccurred())

				otherBuildTwo, err := otherPipelineDB.CreateJobBuild(jobOneConfig.Name)
				Expect(err).NotTo(HaveOccurred())

				otherBuildThree, err := otherPipelineDB.CreateJobBuild(jobOneTwoConfig.Name)
				Expect(err).NotTo(HaveOccurred())

				_, err = dbConn.Query(`
				UPDATE builds
				SET inputs_determined = true
				WHERE id in ($1, $2, $3, $4, $5, $6)
			`, buildOne.ID, buildTwo.ID, buildThree.ID, otherBuildOne.ID, otherBuildTwo.ID, otherBuildThree.ID)

				Expect(err).NotTo(HaveOccurred())

				build, found, err := pipelineDB.GetNextPendingBuildBySerialGroup(jobOneConfig.Name, []string{"one"})
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())
				Expect(build.ID).To(Equal(buildOne.ID))

				build, found, err = pipelineDB.GetNextPendingBuildBySerialGroup(jobOneTwoConfig.Name, []string{"one", "two"})
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())
				Expect(build.ID).To(Equal(buildOne.ID))

				scheduled, err := pipelineDB.UpdateBuildToScheduled(buildOne.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(scheduled).To(BeTrue())
				Expect(sqlDB.FinishBuild(buildOne.ID, db.StatusSucceeded)).To(Succeed())

				build, found, err = pipelineDB.GetNextPendingBuildBySerialGroup(jobOneConfig.Name, []string{"one"})
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())
				Expect(build.ID).To(Equal(buildTwo.ID))

				build, found, err = pipelineDB.GetNextPendingBuildBySerialGroup(jobOneTwoConfig.Name, []string{"one", "two"})
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())
				Expect(build.ID).To(Equal(buildTwo.ID))

				scheduled, err = pipelineDB.UpdateBuildToScheduled(buildTwo.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(scheduled).To(BeTrue())
				Expect(sqlDB.FinishBuild(buildTwo.ID, db.StatusSucceeded)).To(Succeed())

				build, found, err = otherPipelineDB.GetNextPendingBuildBySerialGroup(jobOneConfig.Name, []string{"one"})
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())
				Expect(build.ID).To(Equal(otherBuildOne.ID))

				build, found, err = otherPipelineDB.GetNextPendingBuildBySerialGroup(jobOneTwoConfig.Name, []string{"one", "two"})
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())
				Expect(build.ID).To(Equal(otherBuildOne.ID))

				scheduled, err = otherPipelineDB.UpdateBuildToScheduled(otherBuildOne.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(scheduled).To(BeTrue())
				Expect(sqlDB.FinishBuild(otherBuildOne.ID, db.StatusSucceeded)).To(Succeed())

				build, found, err = otherPipelineDB.GetNextPendingBuildBySerialGroup(jobOneConfig.Name, []string{"one"})
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())
				Expect(build.ID).To(Equal(otherBuildTwo.ID))

				build, found, err = otherPipelineDB.GetNextPendingBuildBySerialGroup(jobOneTwoConfig.Name, []string{"one", "two"})
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())
				Expect(build.ID).To(Equal(otherBuildTwo.ID))

				scheduled, err = otherPipelineDB.UpdateBuildToScheduled(otherBuildTwo.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(scheduled).To(BeTrue())
				Expect(sqlDB.FinishBuild(otherBuildTwo.ID, db.StatusSucceeded)).To(Succeed())

				build, found, err = otherPipelineDB.GetNextPendingBuildBySerialGroup(jobOneConfig.Name, []string{"one"})
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())
				Expect(build.ID).To(Equal(otherBuildThree.ID))
				build, found, err = otherPipelineDB.GetNextPendingBuildBySerialGroup(jobOneTwoConfig.Name, []string{"one", "two"})
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())
				Expect(build.ID).To(Equal(otherBuildThree.ID))

				build, found, err = pipelineDB.GetNextPendingBuildBySerialGroup(jobOneConfig.Name, []string{"one"})
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())
				Expect(build.ID).To(Equal(buildThree.ID))

				build, found, err = pipelineDB.GetNextPendingBuildBySerialGroup(jobOneTwoConfig.Name, []string{"one", "two"})
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())
				Expect(build.ID).To(Equal(buildThree.ID))
			})
		})

		Describe("GetRunningBuildsBySerialGroup", func() {
			//TODO: Delete this before each after #114257887
			BeforeEach(func() {
				_, err := dbConn.Query(`
				INSERT INTO jobs_serial_groups (serial_group, job_id) VALUES
				('serial-group', (select j.id from jobs j, pipelines p where j.name = 'some-job' and j.pipeline_id = p.id and p.name = $1)),
				('serial-group', (select j.id from jobs j, pipelines p where j.name = 'other-serial-group-job' and j.pipeline_id = p.id and p.name = $1)),
				('different-serial-group', (select j.id from jobs j, pipelines p where j.name = 'different-serial-group-job' and j.pipeline_id = p.id and p.name = $1))
			`, pipelineDB.GetPipelineName())
				Expect(err).NotTo(HaveOccurred())
			})

			Describe("same job", func() {
				var startedBuild, scheduledBuild db.Build

				BeforeEach(func() {
					var err error
					_, err = pipelineDB.CreateJobBuild("some-job")
					Expect(err).NotTo(HaveOccurred())

					startedBuild, err = pipelineDB.CreateJobBuild("some-job")
					Expect(err).NotTo(HaveOccurred())
					_, err = sqlDB.StartBuild(startedBuild.ID, "", "")
					Expect(err).NotTo(HaveOccurred())

					scheduledBuild, err = pipelineDB.CreateJobBuild("some-job")
					Expect(err).NotTo(HaveOccurred())

					scheduled, err := pipelineDB.UpdateBuildToScheduled(scheduledBuild.ID)
					Expect(err).NotTo(HaveOccurred())
					Expect(scheduled).To(BeTrue())

					for _, s := range []db.Status{db.StatusSucceeded, db.StatusFailed, db.StatusErrored, db.StatusAborted} {
						finishedBuild, err := pipelineDB.CreateJobBuild("some-job")
						Expect(err).NotTo(HaveOccurred())

						scheduled, err = pipelineDB.UpdateBuildToScheduled(finishedBuild.ID)
						Expect(err).NotTo(HaveOccurred())
						Expect(scheduled).To(BeTrue())
						err = sqlDB.FinishBuild(finishedBuild.ID, s)
					}

					_, err = pipelineDB.CreateJobBuild("some-other-job")
					Expect(err).NotTo(HaveOccurred())
				})

				It("returns a list of running or schedule builds for said job", func() {
					builds, err := pipelineDB.GetRunningBuildsBySerialGroup("some-job", []string{"serial-group"})
					Expect(err).NotTo(HaveOccurred())

					Expect(len(builds)).To(Equal(2))
					ids := []int{}
					for _, build := range builds {
						ids = append(ids, build.ID)
					}
					Expect(ids).To(ConsistOf([]int{startedBuild.ID, scheduledBuild.ID}))
				})
			})

			Describe("multiple jobs with same serial group", func() {
				var serialGroupBuild db.Build

				BeforeEach(func() {
					var err error
					_, err = pipelineDB.CreateJobBuild("some-job")
					Expect(err).NotTo(HaveOccurred())

					serialGroupBuild, err = pipelineDB.CreateJobBuild("other-serial-group-job")
					Expect(err).NotTo(HaveOccurred())

					scheduled, err := pipelineDB.UpdateBuildToScheduled(serialGroupBuild.ID)
					Expect(err).NotTo(HaveOccurred())
					Expect(scheduled).To(BeTrue())

					differentSerialGroupBuild, err := pipelineDB.CreateJobBuild("different-serial-group-job")
					Expect(err).NotTo(HaveOccurred())

					scheduled, err = pipelineDB.UpdateBuildToScheduled(differentSerialGroupBuild.ID)
					Expect(err).NotTo(HaveOccurred())
					Expect(scheduled).To(BeTrue())
				})

				It("returns a list of builds in the same serial group", func() {
					builds, err := pipelineDB.GetRunningBuildsBySerialGroup("some-job", []string{"serial-group"})
					Expect(err).NotTo(HaveOccurred())

					Expect(len(builds)).To(Equal(1))
					Expect(builds[0].ID).To(Equal(serialGroupBuild.ID))
				})
			})
		})

		Context("when a build is created for a job", func() {
			var build1 db.Build
			var jobConfig atc.JobConfig

			BeforeEach(func() {
				var err error
				build1, err = pipelineDB.CreateJobBuild("some-job")

				jobConfig = atc.JobConfig{
					Name:   "some-job",
					Serial: false,
				}
				Expect(err).NotTo(HaveOccurred())

				dbJob, err := pipelineDB.GetJob("some-job")
				Expect(err).NotTo(HaveOccurred())

				Expect(build1.ID).NotTo(BeZero())
				Expect(build1.JobID).To(Equal(dbJob.ID))
				Expect(build1.JobName).To(Equal("some-job"))
				Expect(build1.Name).To(Equal("1"))
				Expect(build1.Status).To(Equal(db.StatusPending))
				Expect(build1.Scheduled).To(BeFalse())
			})

			It("can be read back as the same object", func() {
				gotBuild, found, err := sqlDB.GetBuild(build1.ID)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())
				Expect(gotBuild).To(Equal(build1))
			})

			It("becomes the current build", func() {
				currentBuild, found, err := pipelineDB.GetCurrentBuild("some-job")
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())
				Expect(currentBuild).To(Equal(build1))
			})

			It("becomes the next pending build", func() {
				nextPending, found, err := pipelineDB.GetNextPendingBuild("some-job")
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())
				Expect(nextPending).To(Equal(build1))
			})

			It("is returned in the job's builds", func() {
				Expect(pipelineDB.GetAllJobBuilds("some-job")).To(ConsistOf([]db.Build{build1}))
			})

			Context("and another build for a different pipeline is created with the same job name", func() {
				BeforeEach(func() {
					otherBuild, err := otherPipelineDB.CreateJobBuild("some-job")
					Expect(err).NotTo(HaveOccurred())
					dbJob, err := otherPipelineDB.GetJob("some-job")
					Expect(err).NotTo(HaveOccurred())

					Expect(otherBuild.ID).NotTo(BeZero())
					Expect(otherBuild.JobID).To(Equal(dbJob.ID))
					Expect(otherBuild.JobName).To(Equal("some-job"))
					Expect(otherBuild.Name).To(Equal("1"))
					Expect(otherBuild.Status).To(Equal(db.StatusPending))
					Expect(otherBuild.Scheduled).To(BeFalse())
				})

				It("does not change the current build", func() {
					currentBuild, found, err := pipelineDB.GetCurrentBuild("some-job")
					Expect(err).NotTo(HaveOccurred())
					Expect(found).To(BeTrue())
					Expect(currentBuild).To(Equal(build1))
				})

				It("does not change the next pending build", func() {
					nextPending, found, err := pipelineDB.GetNextPendingBuild("some-job")
					Expect(err).NotTo(HaveOccurred())
					Expect(found).To(BeTrue())
					Expect(nextPending).To(Equal(build1))
				})

				It("is not returned in the job's builds", func() {
					Expect(pipelineDB.GetAllJobBuilds("some-job")).To(ConsistOf([]db.Build{build1}))
				})
			})

			Context("when scheduled", func() {
				BeforeEach(func() {
					scheduled, err := pipelineDB.UpdateBuildToScheduled(build1.ID)
					Expect(err).NotTo(HaveOccurred())
					Expect(scheduled).To(BeTrue())
					build1.Scheduled = true
				})

				It("remains the current build", func() {
					currentBuild, found, err := pipelineDB.GetCurrentBuild("some-job")
					Expect(err).NotTo(HaveOccurred())
					Expect(found).To(BeTrue())
					Expect(currentBuild).To(Equal(build1))
				})

				It("remains the next pending build", func() {
					nextPending, found, err := pipelineDB.GetNextPendingBuild("some-job")
					Expect(err).NotTo(HaveOccurred())
					Expect(found).To(BeTrue())
					Expect(nextPending).To(Equal(build1))
				})
			})

			Context("when started", func() {
				BeforeEach(func() {
					started, err := sqlDB.StartBuild(build1.ID, "some-engine", "some-metadata")
					Expect(err).NotTo(HaveOccurred())
					Expect(started).To(BeTrue())
				})

				It("saves the updated status, and the engine and engine metadata", func() {
					currentBuild, found, err := pipelineDB.GetCurrentBuild("some-job")
					Expect(err).NotTo(HaveOccurred())
					Expect(found).To(BeTrue())
					Expect(currentBuild.Status).To(Equal(db.StatusStarted))
					Expect(currentBuild.Engine).To(Equal("some-engine"))
					Expect(currentBuild.EngineMetadata).To(Equal("some-metadata"))
				})

				It("saves the build's start time", func() {
					currentBuild, found, err := pipelineDB.GetCurrentBuild("some-job")
					Expect(err).NotTo(HaveOccurred())
					Expect(found).To(BeTrue())
					Expect(currentBuild.StartTime.Unix()).To(BeNumerically("~", time.Now().Unix(), 3))
				})
			})

			Context("when the build finishes", func() {
				BeforeEach(func() {
					err := sqlDB.FinishBuild(build1.ID, db.StatusSucceeded)
					Expect(err).NotTo(HaveOccurred())
				})

				It("sets the build's status and end time", func() {
					currentBuild, found, err := pipelineDB.GetCurrentBuild("some-job")
					Expect(err).NotTo(HaveOccurred())
					Expect(found).To(BeTrue())
					Expect(currentBuild.Status).To(Equal(db.StatusSucceeded))
					Expect(currentBuild.EndTime.Unix()).To(BeNumerically("~", time.Now().Unix(), 3))
				})
			})

			Context("and another is created for the same job", func() {
				var build2 db.Build

				BeforeEach(func() {
					var err error
					build2, err = pipelineDB.CreateJobBuild("some-job")
					Expect(err).NotTo(HaveOccurred())

					Expect(build2.ID).NotTo(BeZero())
					Expect(build2.ID).NotTo(Equal(build1.ID))
					Expect(build2.Name).To(Equal("2"))
					Expect(build2.Status).To(Equal(db.StatusPending))
				})

				It("can also be read back as the same object", func() {
					gotBuild, found, err := sqlDB.GetBuild(build2.ID)
					Expect(err).NotTo(HaveOccurred())
					Expect(found).To(BeTrue())
					Expect(gotBuild).To(Equal(build2))
				})

				It("is returned in the job's builds, before the rest", func() {
					Expect(pipelineDB.GetAllJobBuilds("some-job")).To(Equal([]db.Build{
						build2,
						build1,
					}))
				})

				Describe("the first build", func() {
					It("remains the next pending build", func() {
						nextPending, found, err := pipelineDB.GetNextPendingBuild("some-job")
						Expect(err).NotTo(HaveOccurred())
						Expect(found).To(BeTrue())
						Expect(nextPending).To(Equal(build1))
					})

					It("remains the current build", func() {
						currentBuild, found, err := pipelineDB.GetCurrentBuild("some-job")
						Expect(err).NotTo(HaveOccurred())
						Expect(found).To(BeTrue())
						Expect(currentBuild).To(Equal(build1))
					})
				})
			})

			Context("and another is created for a different job", func() {
				var otherJobBuild db.Build

				BeforeEach(func() {
					var err error

					otherJobBuild, err = pipelineDB.CreateJobBuild("some-other-job")
					Expect(err).NotTo(HaveOccurred())

					Expect(otherJobBuild.ID).NotTo(BeZero())
					Expect(otherJobBuild.Name).To(Equal("1"))
					Expect(otherJobBuild.Status).To(Equal(db.StatusPending))
				})

				It("shows up in its job's builds", func() {
					Expect(pipelineDB.GetAllJobBuilds("some-other-job")).To(Equal([]db.Build{otherJobBuild}))
				})

				It("does not show up in the first build's job's builds", func() {
					Expect(pipelineDB.GetAllJobBuilds("some-job")).To(Equal([]db.Build{build1}))
				})
			})
		})

		Describe("determining the inputs for a job", func() {
			It("can still be scheduled with no inputs", func() {
				buildInputs, found, err := loadAndGetLatestInputVersions("third-job", []config.JobInput{})
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())

				Expect(buildInputs).To(Equal([]db.BuildInput{}))
			})

			It("ensures that when scanning for previous inputs versions it only considers those from the same job", func() {
				resource, err := pipelineDB.GetResource("some-resource")
				Expect(err).NotTo(HaveOccurred())

				err = pipelineDB.SaveResourceVersions(atc.ResourceConfig{
					Name:   "some-resource",
					Type:   "some-type",
					Source: atc.Source{"some": "source"},
				}, []atc.Version{{"version": "1"}})
				Expect(err).NotTo(HaveOccurred())

				savedVR1, found, err := pipelineDB.GetLatestVersionedResource(resource)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())

				otherResource, err := pipelineDB.GetResource("some-other-resource")
				Expect(err).NotTo(HaveOccurred())

				err = pipelineDB.SaveResourceVersions(atc.ResourceConfig{
					Name:   "some-other-resource",
					Type:   "some-type",
					Source: atc.Source{"some": "source"},
				}, []atc.Version{{"version": "1"}})
				Expect(err).NotTo(HaveOccurred())

				otherSavedVR1, found, err := pipelineDB.GetLatestVersionedResource(otherResource)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())

				err = pipelineDB.SaveResourceVersions(atc.ResourceConfig{
					Name:   "some-resource",
					Type:   "some-type",
					Source: atc.Source{"some": "source"},
				}, []atc.Version{{"version": "2"}})
				Expect(err).NotTo(HaveOccurred())

				savedVR2, found, err := pipelineDB.GetLatestVersionedResource(resource)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())

				err = pipelineDB.SaveResourceVersions(atc.ResourceConfig{
					Name:   "some-other-resource",
					Type:   "some-type",
					Source: atc.Source{"some": "source"},
				}, []atc.Version{{"version": "2"}})
				Expect(err).NotTo(HaveOccurred())

				otherSavedVR2, found, err := pipelineDB.GetLatestVersionedResource(otherResource)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())

				err = pipelineDB.SaveResourceVersions(atc.ResourceConfig{
					Name:   "some-resource",
					Type:   "some-type",
					Source: atc.Source{"some": "source"},
				}, []atc.Version{{"version": "3"}})
				Expect(err).NotTo(HaveOccurred())

				savedVR3, found, err := pipelineDB.GetLatestVersionedResource(resource)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())

				err = pipelineDB.SaveResourceVersions(atc.ResourceConfig{
					Name:   "some-other-resource",
					Type:   "some-type",
					Source: atc.Source{"some": "source"},
				}, []atc.Version{{"version": "3"}})
				Expect(err).NotTo(HaveOccurred())

				otherSavedVR3, found, err := pipelineDB.GetLatestVersionedResource(otherResource)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())

				build1, err := pipelineDB.CreateJobBuild("a-job")
				Expect(err).NotTo(HaveOccurred())

				_, err = pipelineDB.SaveBuildInput(build1.ID, db.BuildInput{
					Name:              "some-input-name",
					VersionedResource: savedVR1.VersionedResource,
				})
				Expect(err).NotTo(HaveOccurred())

				_, err = pipelineDB.SaveBuildOutput(build1.ID, savedVR1.VersionedResource, false)
				Expect(err).NotTo(HaveOccurred())

				_, err = pipelineDB.SaveBuildInput(build1.ID, db.BuildInput{
					Name:              "some-other-input-name",
					VersionedResource: otherSavedVR1.VersionedResource,
				})
				Expect(err).NotTo(HaveOccurred())

				_, err = pipelineDB.SaveBuildOutput(build1.ID, otherSavedVR1.VersionedResource, false)
				Expect(err).NotTo(HaveOccurred())

				otherBuild2, err := pipelineDB.CreateJobBuild("some-other-job")
				Expect(err).NotTo(HaveOccurred())

				_, err = pipelineDB.SaveBuildInput(otherBuild2.ID, db.BuildInput{
					Name:              "some-input-name",
					VersionedResource: savedVR2.VersionedResource,
				})
				Expect(err).NotTo(HaveOccurred())

				_, err = pipelineDB.SaveBuildOutput(otherBuild2.ID, savedVR2.VersionedResource, false)
				Expect(err).NotTo(HaveOccurred())

				_, err = pipelineDB.SaveBuildInput(otherBuild2.ID, db.BuildInput{
					Name:              "some-other-input-name",
					VersionedResource: otherSavedVR2.VersionedResource,
				})
				Expect(err).NotTo(HaveOccurred())

				_, err = pipelineDB.SaveBuildOutput(otherBuild2.ID, otherSavedVR2.VersionedResource, false)
				Expect(err).NotTo(HaveOccurred())

				build3, err := pipelineDB.CreateJobBuild("a-job")
				Expect(err).NotTo(HaveOccurred())

				_, err = pipelineDB.SaveBuildInput(build3.ID, db.BuildInput{
					Name:              "some-input-name",
					VersionedResource: savedVR3.VersionedResource,
				})
				Expect(err).NotTo(HaveOccurred())

				_, err = pipelineDB.SaveBuildInput(build3.ID, db.BuildInput{
					Name:              "some-other-input-name",
					VersionedResource: otherSavedVR3.VersionedResource,
				})
				Expect(err).NotTo(HaveOccurred())

				err = sqlDB.FinishBuild(build1.ID, db.StatusSucceeded)
				Expect(err).NotTo(HaveOccurred())
				err = sqlDB.FinishBuild(otherBuild2.ID, db.StatusSucceeded)
				Expect(err).NotTo(HaveOccurred())
				err = sqlDB.FinishBuild(build3.ID, db.StatusSucceeded)
				Expect(err).NotTo(HaveOccurred())

				jobBuildInputs := []config.JobInput{
					{
						Name:     "some-input-name",
						Resource: "some-resource",
						Passed:   []string{"a-job"},
					},
					{
						Name:     "some-other-input-name",
						Resource: "some-other-resource",
					},
				}

				versions, found, err := loadAndGetLatestInputVersions("third-job", jobBuildInputs)
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())
				Expect(len(versions)).To(Equal(2))

				var someInput, someOtherInput db.BuildInput
				if versions[0].Name == "some-input-name" {
					someInput = versions[0]
					someOtherInput = versions[1]
				} else {
					someInput = versions[1]
					someOtherInput = versions[0]
				}

				Expect(someInput.Name).To(Equal("some-input-name"))
				Expect(someInput.VersionedResource.Resource).To(Equal(savedVR1.VersionedResource.Resource))
				Expect(someInput.VersionedResource.Type).To(Equal(savedVR1.VersionedResource.Type))
				Expect(someInput.VersionedResource.Version).To(Equal(savedVR1.VersionedResource.Version))
				Expect(someInput.VersionedResource.Metadata).To(Equal(savedVR1.VersionedResource.Metadata))
				Expect(someInput.VersionedResource.PipelineName).To(Equal(savedVR1.VersionedResource.PipelineName))

				Expect(someOtherInput.Name).To(Equal("some-other-input-name"))
				Expect(someOtherInput.VersionedResource.Resource).To(Equal(otherSavedVR3.VersionedResource.Resource))
				Expect(someOtherInput.VersionedResource.Type).To(Equal(savedVR3.VersionedResource.Type))
				Expect(someOtherInput.VersionedResource.Version).To(Equal(savedVR3.VersionedResource.Version))
				Expect(someOtherInput.VersionedResource.Metadata).To(Equal(savedVR3.VersionedResource.Metadata))
				Expect(someOtherInput.VersionedResource.PipelineName).To(Equal(savedVR3.VersionedResource.PipelineName))
			})

			It("ensures that versions from jobs mentioned in two input's 'passed' sections came from the same successful builds", func() {
				j1b1, err := pipelineDB.CreateJobBuild("some-job")
				Expect(err).NotTo(HaveOccurred())

				j2b1, err := pipelineDB.CreateJobBuild("some-other-job")
				Expect(err).NotTo(HaveOccurred())

				sb1, err := pipelineDB.CreateJobBuild("shared-job")
				Expect(err).NotTo(HaveOccurred())

				_, err = otherPipelineDB.CreateJobBuild("some-job")
				Expect(err).NotTo(HaveOccurred())

				_, err = otherPipelineDB.CreateJobBuild("some-other-job")
				Expect(err).NotTo(HaveOccurred())

				_, err = otherPipelineDB.CreateJobBuild("shared-job")
				Expect(err).NotTo(HaveOccurred())

				_, found, err := loadAndGetLatestInputVersions("a-job", []config.JobInput{
					{
						Name:     "input-1",
						Resource: "some-resource",
						Passed:   []string{"shared-job", "some-job"},
					},
					{
						Name:     "input-2",
						Resource: "some-other-resource",
						Passed:   []string{"shared-job", "some-other-job"},
					},
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeFalse())

				_, err = pipelineDB.SaveBuildOutput(sb1.ID, db.VersionedResource{
					Resource: "some-resource",
					Type:     "some-type",
					Version:  db.Version{"v": "r1-common-to-shared-and-j1"},
				}, false)
				Expect(err).NotTo(HaveOccurred())

				_, err = otherPipelineDB.SaveBuildOutput(sb1.ID, db.VersionedResource{
					Resource: "some-resource",
					Type:     "some-type",
					Version:  db.Version{"v": "r1-common-to-shared-and-j1"},
				}, false)
				Expect(err).NotTo(HaveOccurred())

				_, err = pipelineDB.SaveBuildOutput(sb1.ID, db.VersionedResource{
					Resource: "some-other-resource",
					Type:     "some-type",
					Version:  db.Version{"v": "r2-common-to-shared-and-j2"},
				}, false)
				Expect(err).NotTo(HaveOccurred())

				_, err = otherPipelineDB.SaveBuildOutput(sb1.ID, db.VersionedResource{
					Resource: "some-other-resource",
					Type:     "some-type",
					Version:  db.Version{"v": "r2-common-to-shared-and-j2"},
				}, false)
				Expect(err).NotTo(HaveOccurred())

				savedVR1, err := pipelineDB.SaveBuildOutput(j1b1.ID, db.VersionedResource{
					Resource: "some-resource",
					Type:     "some-type",
					Version:  db.Version{"v": "r1-common-to-shared-and-j1"},
				}, false)
				Expect(err).NotTo(HaveOccurred())

				_, err = otherPipelineDB.SaveBuildOutput(j1b1.ID, db.VersionedResource{
					Resource: "some-resource",
					Type:     "some-type",
					Version:  db.Version{"v": "r1-common-to-shared-and-j1"},
				}, false)
				Expect(err).NotTo(HaveOccurred())

				savedVR2, err := pipelineDB.SaveBuildOutput(j2b1.ID, db.VersionedResource{
					Resource: "some-other-resource",
					Type:     "some-type",
					Version:  db.Version{"v": "r2-common-to-shared-and-j2"},
				}, false)
				Expect(err).NotTo(HaveOccurred())

				_, err = otherPipelineDB.SaveBuildOutput(j2b1.ID, db.VersionedResource{
					Resource: "some-other-resource",
					Type:     "some-type",
					Version:  db.Version{"v": "r2-common-to-shared-and-j2"},
				}, false)
				Expect(err).NotTo(HaveOccurred())

				err = sqlDB.FinishBuild(sb1.ID, db.StatusSucceeded)
				Expect(err).NotTo(HaveOccurred())
				err = sqlDB.FinishBuild(j1b1.ID, db.StatusSucceeded)
				Expect(err).NotTo(HaveOccurred())
				err = sqlDB.FinishBuild(j2b1.ID, db.StatusSucceeded)
				Expect(err).NotTo(HaveOccurred())

				versions, found, err := loadAndGetLatestInputVersions("a-job", []config.JobInput{
					{
						Name:     "input-1",
						Resource: "some-resource",
						Passed:   []string{"shared-job", "some-job"},
					},
					{
						Name:     "input-2",
						Resource: "some-other-resource",
						Passed:   []string{"shared-job", "some-other-job"},
					},
				})
				Expect(found).To(BeTrue())
				Expect(versions).To(ConsistOf([]db.BuildInput{
					{
						Name:              "input-1",
						VersionedResource: savedVR1.VersionedResource,
					},
					{
						Name:              "input-2",
						VersionedResource: savedVR2.VersionedResource,
					},
				}))

				sb2, err := pipelineDB.CreateJobBuild("shared-job")
				Expect(err).NotTo(HaveOccurred())

				j1b2, err := pipelineDB.CreateJobBuild("some-job")
				Expect(err).NotTo(HaveOccurred())

				j2b2, err := pipelineDB.CreateJobBuild("some-other-job")
				Expect(err).NotTo(HaveOccurred())

				savedCommonVR1, err := pipelineDB.SaveBuildOutput(sb2.ID, db.VersionedResource{
					Resource: "some-resource",
					Type:     "some-type",
					Version:  db.Version{"v": "new-r1-common-to-shared-and-j1"},
				}, false)
				Expect(err).NotTo(HaveOccurred())

				_, err = pipelineDB.SaveBuildOutput(sb2.ID, db.VersionedResource{
					Resource: "some-other-resource",
					Type:     "some-type",
					Version:  db.Version{"v": "new-r2-common-to-shared-and-j2"},
				}, false)
				Expect(err).NotTo(HaveOccurred())

				savedCommonVR1, err = pipelineDB.SaveBuildOutput(j1b2.ID, db.VersionedResource{
					Resource: "some-resource",
					Type:     "some-type",
					Version:  db.Version{"v": "new-r1-common-to-shared-and-j1"},
				}, false)
				Expect(err).NotTo(HaveOccurred())

				// do NOT save some-other-resource as an output of job-2

				versions, found, err = loadAndGetLatestInputVersions("a-job", []config.JobInput{
					{
						Name:     "input-1",
						Resource: "some-resource",
						Passed:   []string{"shared-job", "some-job"},
					},
					{
						Name:     "input-2",
						Resource: "some-other-resource",
						Passed:   []string{"shared-job", "some-other-job"},
					},
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())
				Expect(versions).To(ConsistOf([]db.BuildInput{
					{
						Name:              "input-1",
						VersionedResource: savedVR1.VersionedResource,
					},
					{
						Name:              "input-2",
						VersionedResource: savedVR2.VersionedResource,
					},
				}))

				// now save the output of some-other-resource job-2
				savedCommonVR2, err := pipelineDB.SaveBuildOutput(j2b2.ID, db.VersionedResource{
					Resource: "some-other-resource",
					Type:     "some-type",
					Version:  db.Version{"v": "new-r2-common-to-shared-and-j2"},
				}, false)
				Expect(err).NotTo(HaveOccurred())

				err = sqlDB.FinishBuild(sb2.ID, db.StatusSucceeded)
				Expect(err).NotTo(HaveOccurred())
				err = sqlDB.FinishBuild(j1b2.ID, db.StatusSucceeded)
				Expect(err).NotTo(HaveOccurred())
				err = sqlDB.FinishBuild(j2b2.ID, db.StatusSucceeded)
				Expect(err).NotTo(HaveOccurred())

				versions, found, err = loadAndGetLatestInputVersions("a-job", []config.JobInput{
					{
						Name:     "input-1",
						Resource: "some-resource",
						Passed:   []string{"shared-job", "some-job"},
					},
					{
						Name:     "input-2",
						Resource: "some-other-resource",
						Passed:   []string{"shared-job", "some-other-job"},
					},
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())
				Expect(versions).To(ConsistOf([]db.BuildInput{
					{
						Name:              "input-1",
						VersionedResource: savedCommonVR1.VersionedResource,
					},
					{
						Name:              "input-2",
						VersionedResource: savedCommonVR2.VersionedResource,
					},
				}))

				j2b3, err := pipelineDB.CreateJobBuild("some-other-job")
				Expect(err).NotTo(HaveOccurred())

				_, err = pipelineDB.SaveBuildOutput(j2b3.ID, db.VersionedResource{
					Resource: "some-other-resource",
					Type:     "some-type",
					Version:  db.Version{"v": "should-not-be-emitted-because-of-failure"},
				}, false)
				Expect(err).NotTo(HaveOccurred())

				// Fail the 3rd build of the 2nd job, this should put the versions back to the previous set

				err = sqlDB.FinishBuild(j2b3.ID, db.StatusFailed)
				Expect(err).NotTo(HaveOccurred())

				versions, found, err = loadAndGetLatestInputVersions("a-job", []config.JobInput{
					{
						Name:     "input-2",
						Resource: "some-other-resource",
						Passed:   []string{"some-other-job"},
					},
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())
				Expect(versions).To(ConsistOf([]db.BuildInput{
					{
						Name:              "input-2",
						VersionedResource: savedCommonVR2.VersionedResource,
					},
				}))

				// save newer versions; should be new latest
				for i := 0; i < 10; i++ {
					version := fmt.Sprintf("version-%d", i+1)

					savedCommonVR1, err := pipelineDB.SaveBuildOutput(sb1.ID, db.VersionedResource{
						Resource: "some-resource",
						Type:     "some-type",
						Version:  db.Version{"v": version + "-r1-common-to-shared-and-j1"},
					}, false)
					Expect(err).NotTo(HaveOccurred())

					savedCommonVR2, err := pipelineDB.SaveBuildOutput(sb1.ID, db.VersionedResource{
						Resource: "some-other-resource",
						Type:     "some-type",
						Version:  db.Version{"v": version + "-r2-common-to-shared-and-j2"},
					}, false)
					Expect(err).NotTo(HaveOccurred())

					savedCommonVR1, err = pipelineDB.SaveBuildOutput(j1b1.ID, db.VersionedResource{
						Resource: "some-resource",
						Type:     "some-type",
						Version:  db.Version{"v": version + "-r1-common-to-shared-and-j1"},
					}, false)
					Expect(err).NotTo(HaveOccurred())

					savedCommonVR2, err = pipelineDB.SaveBuildOutput(j2b1.ID, db.VersionedResource{
						Resource: "some-other-resource",
						Type:     "some-type",
						Version:  db.Version{"v": version + "-r2-common-to-shared-and-j2"},
					}, false)
					Expect(err).NotTo(HaveOccurred())

					versions, found, err := loadAndGetLatestInputVersions("a-job", []config.JobInput{
						{
							Name:     "input-1",
							Resource: "some-resource",
							Passed:   []string{"shared-job", "some-job"},
						},
						{
							Name:     "input-2",
							Resource: "some-other-resource",
							Passed:   []string{"shared-job", "some-other-job"},
						},
					})
					Expect(err).NotTo(HaveOccurred())
					Expect(found).To(BeTrue())
					Expect(versions).To(ConsistOf([]db.BuildInput{
						{
							Name:              "input-1",
							VersionedResource: savedCommonVR1.VersionedResource,
						},
						{
							Name:              "input-2",
							VersionedResource: savedCommonVR2.VersionedResource,
						},
					}))
				}
			})
		})

		It("can report a job's latest running and finished builds", func() {
			finished, next, err := pipelineDB.GetJobFinishedAndNextBuild("some-job")
			Expect(err).NotTo(HaveOccurred())

			Expect(next).To(BeNil())
			Expect(finished).To(BeNil())

			finishedBuild, err := pipelineDB.CreateJobBuild("some-job")
			Expect(err).NotTo(HaveOccurred())

			err = sqlDB.FinishBuild(finishedBuild.ID, db.StatusSucceeded)
			Expect(err).NotTo(HaveOccurred())

			otherFinishedBuild, err := otherPipelineDB.CreateJobBuild("some-job")
			Expect(err).NotTo(HaveOccurred())

			err = sqlDB.FinishBuild(otherFinishedBuild.ID, db.StatusSucceeded)
			Expect(err).NotTo(HaveOccurred())

			finished, next, err = pipelineDB.GetJobFinishedAndNextBuild("some-job")
			Expect(err).NotTo(HaveOccurred())

			Expect(next).To(BeNil())
			Expect(finished.ID).To(Equal(finishedBuild.ID))

			nextBuild, err := pipelineDB.CreateJobBuild("some-job")
			Expect(err).NotTo(HaveOccurred())

			started, err := sqlDB.StartBuild(nextBuild.ID, "some-engine", "meta")
			Expect(err).NotTo(HaveOccurred())
			Expect(started).To(BeTrue())

			otherNextBuild, err := otherPipelineDB.CreateJobBuild("some-job")
			Expect(err).NotTo(HaveOccurred())

			otherStarted, err := sqlDB.StartBuild(otherNextBuild.ID, "some-engine", "meta")
			Expect(err).NotTo(HaveOccurred())
			Expect(otherStarted).To(BeTrue())

			finished, next, err = pipelineDB.GetJobFinishedAndNextBuild("some-job")
			Expect(err).NotTo(HaveOccurred())

			Expect(next.ID).To(Equal(nextBuild.ID))
			Expect(finished.ID).To(Equal(finishedBuild.ID))

			anotherRunningBuild, err := pipelineDB.CreateJobBuild("some-job")
			Expect(err).NotTo(HaveOccurred())

			finished, next, err = pipelineDB.GetJobFinishedAndNextBuild("some-job")
			Expect(err).NotTo(HaveOccurred())

			Expect(next.ID).To(Equal(nextBuild.ID)) // not anotherRunningBuild
			Expect(finished.ID).To(Equal(finishedBuild.ID))

			started, err = sqlDB.StartBuild(anotherRunningBuild.ID, "some-engine", "meta")
			Expect(err).NotTo(HaveOccurred())
			Expect(started).To(BeTrue())

			finished, next, err = pipelineDB.GetJobFinishedAndNextBuild("some-job")
			Expect(err).NotTo(HaveOccurred())

			Expect(next.ID).To(Equal(nextBuild.ID)) // not anotherRunningBuild
			Expect(finished.ID).To(Equal(finishedBuild.ID))

			err = sqlDB.FinishBuild(nextBuild.ID, db.StatusSucceeded)
			Expect(err).NotTo(HaveOccurred())

			finished, next, err = pipelineDB.GetJobFinishedAndNextBuild("some-job")
			Expect(err).NotTo(HaveOccurred())

			Expect(next.ID).To(Equal(anotherRunningBuild.ID))
			Expect(finished.ID).To(Equal(nextBuild.ID))
		})
	})
})
