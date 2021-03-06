package migrations

import "github.com/BurntSushi/migration"

func AddIndexesToABunchOfStuff(tx migration.LimitedTx) error {
	_, err := tx.Exec(`
		CREATE INDEX build_inputs_build_id_versioned_resource_id ON build_inputs (build_id, versioned_resource_id);
		CREATE INDEX build_outputs_build_id_versioned_resource_id ON build_outputs (build_id, versioned_resource_id);
		CREATE INDEX builds_job_id ON builds (job_id);
		CREATE INDEX jobs_pipeline_id ON jobs (pipeline_id);
		CREATE INDEX resources_pipeline_id ON resources (pipeline_id);
	`)
	if err != nil {
		return err
	}

	return nil
}
