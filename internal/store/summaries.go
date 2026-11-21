package store

import (
	"context"
	"database/sql"
	"time"
)

// ProjectSummary is what the project list shows besides the project itself.
type ProjectSummary struct {
	AppsRunning      int        `json:"apps_running"` // set to run and deployed
	AppsStopped      int        `json:"apps_stopped"`
	AppsNotDeployed  int        `json:"apps_not_deployed"`
	Databases        int        `json:"databases"`
	Domains          int        `json:"domains"` // hostnames routed to its apps
	LastDeployAt     *time.Time `json:"last_deploy_at"`
	LastDeployStatus string     `json:"last_deploy_status"`
	LastDeployApp    string     `json:"last_deploy_app"`
}

// ProjectSummaries returns a summary per project id, in one query.
func (s *Store) ProjectSummaries(ctx context.Context) (map[string]ProjectSummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT p.id,
 (SELECT COUNT(*) FROM apps a WHERE a.project_id=p.id AND a.desired_state='running' AND a.current_deployment_id IS NOT NULL AND a.current_deployment_id<>''),
 (SELECT COUNT(*) FROM apps a WHERE a.project_id=p.id AND a.desired_state<>'running' AND a.current_deployment_id IS NOT NULL AND a.current_deployment_id<>''),
 (SELECT COUNT(*) FROM apps a WHERE a.project_id=p.id AND (a.current_deployment_id IS NULL OR a.current_deployment_id='')),
 (SELECT COUNT(*) FROM db_instances d WHERE d.project_id=p.id),
 (SELECT COUNT(DISTINCT r.hostname) FROM routes r JOIN apps a ON a.id=r.app_id WHERE a.project_id=p.id),
 l.created_at, COALESCE(l.status,''), COALESCE(l.name,'')
FROM projects p LEFT JOIN (
 SELECT a.project_id, d.created_at, d.status, a.name,
  ROW_NUMBER() OVER (PARTITION BY a.project_id ORDER BY d.id DESC) AS n
 FROM deployments d JOIN apps a ON a.id=d.app_id) l ON l.project_id=p.id AND l.n=1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]ProjectSummary{}
	for rows.Next() {
		var id string
		var p ProjectSummary
		var at sql.NullString
		if err := rows.Scan(&id, &p.AppsRunning, &p.AppsStopped, &p.AppsNotDeployed, &p.Databases, &p.Domains, &at, &p.LastDeployStatus, &p.LastDeployApp); err != nil {
			return nil, err
		}
		if at.Valid {
			t := parseTime(at.String)
			p.LastDeployAt = &t
		}
		out[id] = p
	}
	return out, rows.Err()
}

// DBInstanceSummary is what the database list shows besides the instance itself.
type DBInstanceSummary struct {
	Databases        int        `json:"database_count"`
	Users            int        `json:"user_count"`
	BackupPolicies   int        `json:"backup_policies"` // enabled schedules
	LastBackupAt     *time.Time `json:"last_backup_at"`
	LastBackupStatus string     `json:"last_backup_status"`
}

// DBInstanceSummaries returns a summary per instance id, in one query.
func (s *Store) DBInstanceSummaries(ctx context.Context) (map[string]DBInstanceSummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT i.id,
 (SELECT COUNT(*) FROM db_databases x WHERE x.instance_id=i.id),
 (SELECT COUNT(*) FROM db_users x WHERE x.instance_id=i.id),
 (SELECT COUNT(*) FROM backup_policies x WHERE x.instance_id=i.id AND x.enabled=1),
 (SELECT b.created_at FROM db_backups b WHERE b.instance_id=i.id ORDER BY b.id DESC LIMIT 1),
 COALESCE((SELECT o.status FROM db_backups b JOIN db_operations o ON o.id=b.id WHERE b.instance_id=i.id ORDER BY b.id DESC LIMIT 1),'')
FROM db_instances i`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]DBInstanceSummary{}
	for rows.Next() {
		var id string
		var d DBInstanceSummary
		var at sql.NullString
		if err := rows.Scan(&id, &d.Databases, &d.Users, &d.BackupPolicies, &at, &d.LastBackupStatus); err != nil {
			return nil, err
		}
		if at.Valid {
			t := parseTime(at.String)
			d.LastBackupAt = &t
		}
		out[id] = d
	}
	return out, rows.Err()
}
