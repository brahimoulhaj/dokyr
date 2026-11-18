package store

import "context"

// AppRuntimeTarget deliberately contains no environment, registry or Git secrets.
type AppRuntimeTarget struct {
	AppID         string `json:"app_id"`
	AppName       string `json:"app_name"`
	ProjectID     string `json:"project_id"`
	ProjectName   string `json:"project_name"`
	ContainerName string `json:"-"`
	Port          int    `json:"-"` // the current deployment's, for health checks
	Processes     bool   `json:"-"` // the app has extra processes to watch
	RuntimeState  string `json:"runtime_state"`
}

// AppRuntimeTargets excludes intentionally stopped apps, apps never deployed,
// and running deploys (recreate may intentionally stop the current container).
func (s *Store) AppRuntimeTargets(ctx context.Context) ([]AppRuntimeTarget, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.id, a.name, p.id, p.name, d.container_name, d.port, a.processes <> '[]'
 FROM apps a JOIN projects p ON p.id = a.project_id
 JOIN deployments d ON d.id = a.current_deployment_id
 WHERE a.desired_state = 'running'
 AND NOT EXISTS (SELECT 1 FROM deployments active WHERE active.app_id = a.id AND active.status = 'running')
 ORDER BY p.name, a.name, a.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AppRuntimeTarget{}
	for rows.Next() {
		var target AppRuntimeTarget
		if err := rows.Scan(&target.AppID, &target.AppName, &target.ProjectID, &target.ProjectName, &target.ContainerName, &target.Port, &target.Processes); err != nil {
			return nil, err
		}
		out = append(out, target)
	}
	return out, rows.Err()
}
