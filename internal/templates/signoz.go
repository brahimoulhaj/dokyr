package templates

// SigNoz runs as four apps: its own, the collector that receives OpenTelemetry data, and a
// ClickHouse server with its Keeper. The files are what SigNoz's installer (Foundry) writes
// for Docker Compose, with two changes: ClickHouse asks for a password, since every app of
// the panel shares one network, and SQLite holds SigNoz's own data instead of PostgreSQL.
// What its one-off containers do is folded into start commands: the collector applies the
// schema migrations before it starts, and ClickHouse fetches the histogramQuantile function.
// The collector listens for OTLP only once the first SigNoz account exists, so its port is
// not probed.

const signozClickHouseDSN = "tcp://default:${stack.secret}@${stack.clickhouse.HOST}:9000"

var signoz = Template{
	ID: "signoz", Name: "SigNoz", Category: "monitoring", Website: "https://signoz.io",
	Description: "Traces, metrics and logs over OpenTelemetry, in four apps. Deploy keeper, clickhouse, collector, then this one. OTLP goes to the collector.",
	Image:       "signoz/signoz:v0.151.0", Port: 8080,
	Volumes: vol("data", "/var/lib/signoz"),
	Env: env{
		"SIGNOZ_TELEMETRYSTORE_PROVIDER": "clickhouse", "SIGNOZ_TELEMETRYSTORE_CLICKHOUSE_DSN": signozClickHouseDSN,
		"SIGNOZ_SQLSTORE_SQLITE_PATH": "/var/lib/signoz/signoz.db", "SIGNOZ_ALERTMANAGER_PROVIDER": "signoz",
		"SIGNOZ_TOKENIZER_JWT_SECRET": "${secret}",
	},
	StartPeriod: 120,
	Services: []Service{
		{
			Name: "keeper", Image: "clickhouse/clickhouse-keeper:25.12.5", Port: 9181,
			StartCommand: "exec /usr/bin/clickhouse-keeper --config-file=/etc/clickhouse-keeper/keeper.yaml",
			Volumes:      vol("data", "/var/lib/clickhouse-keeper"),
			Files:        []File{{Target: "/etc/clickhouse-keeper/keeper.yaml", Content: signozKeeper}},
			StartPeriod:  60,
		},
		{
			Name: "clickhouse", Image: "clickhouse/clickhouse-server:25.12.5", Port: 8123,
			// The function is a program ClickHouse runs: fetched once, into the data volume.
			StartCommand: `mkdir -p /var/lib/clickhouse/user_scripts && cd /var/lib/clickhouse/user_scripts && { [ -x histogramQuantile ] || { arch=$(uname -m | sed s/aarch64/arm64/ | sed s/x86_64/amd64/) && wget -qO- "https://github.com/SigNoz/signoz/releases/download/histogram-quantile%2Fv0.0.1/histogram-quantile_linux_$arch.tar.gz" | tar -xz && mv histogram-quantile histogramQuantile; }; } && cd / && exec /entrypoint.sh`,
			Volumes:      vol("data", "/var/lib/clickhouse"),
			Files: []File{
				{Target: "/etc/clickhouse-server/config.yaml", Content: signozClickHouse},
				{Target: "/etc/clickhouse-server/functions.yaml", Content: signozFunctions},
			},
			Env:         env{"CLICKHOUSE_SKIP_USER_SETUP": "1", "CLICKHOUSE_CONFIG": "/etc/clickhouse-server/config.yaml"},
			StartPeriod: 180,
		},
		{
			Name: "collector", Image: "signoz/signoz-otel-collector:v0.144.18", Port: 4318, NoProbe: true,
			StartCommand: "/signoz-otel-collector migrate ready && /signoz-otel-collector migrate bootstrap && /signoz-otel-collector migrate sync up && /signoz-otel-collector migrate async up && exec /signoz-otel-collector --config=/etc/otel-collector-config.yaml --manager-config=/etc/opamp-config.yaml --copy-path=/var/tmp/collector-config.yaml",
			Files: []File{
				{Target: "/etc/otel-collector-config.yaml", Content: signozCollector},
				{Target: "/etc/opamp-config.yaml", Content: signozOpamp},
			},
			Env: env{"SIGNOZ_OTEL_COLLECTOR_CLICKHOUSE_DSN": signozClickHouseDSN, "SIGNOZ_OTEL_COLLECTOR_TIMEOUT": "10m", "LOW_CARDINAL_EXCEPTION_GROUPING": "false"},
		},
	},
}

const signozKeeper = `keeper_server:
  coordination_settings:
    force_sync: false
    operation_timeout_ms: 10000
    raft_logs_level: warning
    session_timeout_ms: 30000
    snapshot_distance: 100000
    snapshots_to_keep: 3
  four_letter_word_white_list: '*'
  log_storage_path: /var/lib/clickhouse-keeper/coordination/log
  raft_configuration:
    server:
    - hostname: ${stack.keeper.HOST}
      id: 0
      port: 9234
  server_id: 0
  snapshot_storage_path: /var/lib/clickhouse-keeper/coordination/snapshots
  tcp_port: 9181
listen_host: 0.0.0.0
logger:
  console: true
  level: information
`

const signozClickHouse = `asynchronous_metric_log:
  ttl: event_date + INTERVAL 1 DAY DELETE
dictionaries_config: '*_dictionary.xml'
display_name: cluster
distributed_ddl:
  path: /clickhouse/task_queue/ddl
error_log:
  ttl: event_date + INTERVAL 1 DAY DELETE
format_schema_path: /var/lib/clickhouse/format_schemas/
http_port: 8123
interserver_http_port: 9009
latency_log:
  ttl: event_date + INTERVAL 1 DAY DELETE
listen_host: 0.0.0.0
logger:
  console: 1
  formatting:
    type: console
  level: information
macros:
  replica: "00"
  shard: "00"
metric_log:
  flush_interval_milliseconds: 30000
  ttl: event_date + INTERVAL 1 DAY DELETE
part_log:
  ttl: event_date + INTERVAL 1 DAY DELETE
path: /var/lib/clickhouse/
processors_profile_log:
  flush_interval_milliseconds: 30000
  ttl: event_date + INTERVAL 1 DAY DELETE
profiles:
  default:
    load_balancing: random
    log_queries: 1
query_log:
  flush_interval_milliseconds: 30000
  partition_by: toYYYYMM(event_date)
  ttl: event_date + INTERVAL 1 DAY DELETE
query_metric_log:
  ttl: event_date + INTERVAL 1 DAY DELETE
query_thread_log:
  ttl: event_date + INTERVAL 1 DAY DELETE
query_views_log:
  ttl: event_date + INTERVAL 1 DAY DELETE
quotas:
  default:
    interval:
      duration: 3600
      errors: 0
      execution_time: 0
      queries: 0
      read_rows: 0
      result_rows: 0
remote_servers:
  cluster:
    shard:
    - replica:
      - host: ${stack.clickhouse.HOST}
        port: 9000
        user: default
        password: ${stack.secret}
session_log:
  ttl: event_date + INTERVAL 1 DAY DELETE
tcp_port: 9000
text_log:
  flush_interval_milliseconds: 30000
  ttl: event_date + INTERVAL 1 DAY DELETE
tmp_path: /var/lib/clickhouse/tmp/
trace_log:
  flush_interval_milliseconds: 30000
  ttl: event_date + INTERVAL 1 DAY DELETE
user_defined_executable_functions_config: '*functions.yaml'
user_directories:
  users_xml:
    path: config.yaml
user_files_path: /var/lib/clickhouse/user_files/
user_scripts_path: /var/lib/clickhouse/user_scripts/
users:
  default:
    access_management: 1
    named_collection_control: 1
    networks:
      ip: ::/0
    password: ${stack.secret}
    profile: default
    quota: default
    show_named_collection: 1
    show_named_collection_secrets: 1
zookeeper:
  node:
  - host: ${stack.keeper.HOST}
    port: 9181
zookeeper_log:
  ttl: event_date + INTERVAL 1 DAY DELETE
`

const signozFunctions = `functions:
  argument:
  - name: buckets
    type: Array(Float64)
  - name: counts
    type: Array(Float64)
  - name: quantile
    type: Float64
  command: ./histogramQuantile
  format: CSV
  name: histogramQuantile
  return_type: Float64
  type: executable
`

// The exporters read the address of ClickHouse from the collector's variable.
const signozCollector = `connectors:
  signozmeter:
    dimensions:
    - name: service.name
    - name: deployment.environment
    - name: host.name
    metrics_flush_interval: 1h
exporters:
  clickhouselogsexporter:
    dsn: ${env:SIGNOZ_OTEL_COLLECTOR_CLICKHOUSE_DSN}/signoz_logs
    sending_queue:
      enabled: false
    timeout: 45s
    use_new_schema: true
  clickhousetraces:
    datasource: ${env:SIGNOZ_OTEL_COLLECTOR_CLICKHOUSE_DSN}/signoz_traces
    low_cardinal_exception_grouping: ${env:LOW_CARDINAL_EXCEPTION_GROUPING}
    sending_queue:
      enabled: false
    timeout: 45s
    use_new_schema: true
  metadataexporter:
    cache:
      provider: in_memory
    dsn: ${env:SIGNOZ_OTEL_COLLECTOR_CLICKHOUSE_DSN}/signoz_metadata
    enabled: true
    timeout: 45s
  signozclickhousemeter:
    dsn: ${env:SIGNOZ_OTEL_COLLECTOR_CLICKHOUSE_DSN}/signoz_meter
    sending_queue:
      enabled: false
    timeout: 45s
  signozclickhousemetrics:
    dsn: ${env:SIGNOZ_OTEL_COLLECTOR_CLICKHOUSE_DSN}/signoz_metrics
    sending_queue:
      enabled: false
    timeout: 45s
extensions:
  pprof:
    endpoint: 0.0.0.0:1777
  signoz_health_check:
    endpoint: 0.0.0.0:13133
processors:
  batch:
    send_batch_max_size: 55000
    send_batch_size: 50000
    timeout: 5s
  batch/meter:
    send_batch_max_size: 25000
    send_batch_size: 20000
    timeout: 5s
  filter/drop_stale:
    error_mode: ignore
    logs:
      log_record:
      - time_unix_nano != 0 and time < Now() - Duration("360h")
      - time_unix_nano == 0 and observed_time_unix_nano != 0 and observed_time < Now() - Duration("360h")
    metrics:
      datapoint:
      - time_unix_nano != 0 and time < Now() - Duration("360h")
    traces:
      span:
      - start_time_unix_nano != 0 and start_time < Now() - Duration("360h")
  signozllmpricing:
    attrs:
      cache_read: gen_ai.usage.cache_read.input_tokens
      cache_write: gen_ai.usage.cache_creation.input_tokens
      in: gen_ai.usage.input_tokens
      model: gen_ai.request.model
      out: gen_ai.usage.output_tokens
    default_pricing:
      rules: []
    output_attrs:
      cache_read: signoz.gen_ai.usage.cache_read.input_tokens.cost
      cache_write: signoz.gen_ai.usage.cache_write.input_tokens.cost
      in: signoz.gen_ai.usage.input_tokens.cost
      out: signoz.gen_ai.usage.output_tokens.cost
      total: signoz.gen_ai.usage.tokens.cost
  signozlogsnormalizer: {}
  signozspanmapper:
    groups: []
  signozspanmetrics/delta:
    aggregation_temporality: AGGREGATION_TEMPORALITY_DELTA
    dimensions:
    - default: default
      name: service.namespace
    - default: default
      name: deployment.environment
    - name: signoz.collector.id
    - name: service.version
    dimensions_cache_size: 100000
    enable_exp_histogram: true
    latency_histogram_buckets: [100us, 1ms, 2ms, 6ms, 10ms, 50ms, 100ms, 250ms, 500ms, 1000ms, 1400ms, 2000ms, 5s, 10s, 20s, 40s, 60s]
    metrics_exporter: signozclickhousemetrics
    metrics_flush_interval: 60s
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
      http:
        endpoint: 0.0.0.0:4318
service:
  extensions:
  - signoz_health_check
  - pprof
  pipelines:
    logs:
      exporters: [clickhouselogsexporter, signozmeter, metadataexporter]
      processors: [signozlogsnormalizer, filter/drop_stale, batch]
      receivers: [otlp]
    metrics:
      exporters: [signozclickhousemetrics, signozmeter, metadataexporter]
      processors: [filter/drop_stale, batch]
      receivers: [otlp]
    metrics/meter:
      exporters: [signozclickhousemeter]
      processors: [batch/meter]
      receivers: [signozmeter]
    traces:
      exporters: [clickhousetraces, signozmeter, metadataexporter]
      processors: [signozspanmetrics/delta, signozspanmapper, signozllmpricing, filter/drop_stale, batch]
      receivers: [otlp]
  telemetry:
    logs:
      encoding: json
`

const signozOpamp = `server_endpoint: ws://${stack.HOST}:4320/v1/opamp
`
