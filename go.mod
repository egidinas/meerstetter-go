module github.com/egidinas/meerstetter-go

go 1.26.2

require go.bug.st/serial v1.6.4 // indirect

require (
	github.com/apache/arrow-go/v18 v18.6.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/egidinas/loom-gossamer-shared/go/arrowutil v0.0.0 // indirect
	github.com/egidinas/loom-gossamer-shared/go/envutil v0.0.0 // indirect
	github.com/egidinas/loom-gossamer-shared/go/metrics v0.0.0 // indirect
	github.com/egidinas/loom-gossamer-shared/go/telemetrytiles/arrow v0.0.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/goccy/go-json v0.10.6 // indirect
	github.com/google/flatbuffers v25.12.19+incompatible // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/klauspost/compress v1.18.5 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/nats-io/nats.go v1.52.0 // indirect
	github.com/nats-io/nkeys v0.4.15 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/pierrec/lz4/v4 v4.1.26 // indirect
	github.com/prometheus/client_golang v1.23.2 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.66.1 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	github.com/zeebo/xxh3 v1.1.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.43.0 // indirect
	go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.43.0 // indirect
	go.opentelemetry.io/otel/metric v1.43.0 // indirect
	go.opentelemetry.io/otel/sdk v1.43.0 // indirect
	go.opentelemetry.io/otel/trace v1.43.0 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	golang.org/x/crypto v0.51.0 // indirect
	golang.org/x/exp v0.0.0-20260112195511-716be5621a96 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

require (
	github.com/creack/goselect v0.1.2 // indirect
	github.com/egidinas/loom-gossamer-shared/go/capabilitycatalog v0.0.0
	github.com/egidinas/loom-gossamer-shared/go/discovery v0.0.0
	github.com/egidinas/loom-gossamer-shared/go/edgepub v0.0.0
	github.com/egidinas/loom-gossamer-shared/go/envelope v0.0.0
	github.com/egidinas/loom-gossamer-shared/go/graphsem v0.0.0
	github.com/egidinas/loom-gossamer-shared/go/graphwall v0.0.0
	github.com/egidinas/loom-gossamer-shared/go/graphwallui v0.0.0
	github.com/egidinas/loom-gossamer-shared/go/ipc v0.0.0
	github.com/egidinas/loom-gossamer-shared/go/lifecycle v0.0.0
	github.com/egidinas/loom-gossamer-shared/go/livebus v0.0.0
	github.com/egidinas/loom-gossamer-shared/go/mathutil v0.0.0 // indirect
	github.com/egidinas/loom-gossamer-shared/go/mesh v0.0.0 // indirect
	github.com/egidinas/loom-gossamer-shared/go/modulekit v0.0.0
	github.com/egidinas/loom-gossamer-shared/go/otelutil v0.0.0
	github.com/egidinas/loom-gossamer-shared/go/safego v0.0.0
	github.com/egidinas/loom-gossamer-shared/go/schema v0.0.0
	github.com/egidinas/signalforge v0.1.0-private.4
	github.com/egidinas/loom-gossamer-shared/go/telemetrytiles v0.0.0
	github.com/egidinas/loom-gossamer-shared/go/tmtc v0.0.0
	github.com/egidinas/loom-gossamer-shared/go/tmtclog v0.0.0
	github.com/egidinas/loom-gossamer-shared/go/transport v0.0.0
	golang.org/x/sys v0.44.0 // indirect
)

replace github.com/egidinas/loom-gossamer-shared/go/graphwallui => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/graphwallui

replace github.com/egidinas/loom-gossamer-shared/go/contracts => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/contracts

replace github.com/egidinas/loom-gossamer-shared/go/graphwall => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/graphwall

replace github.com/egidinas/loom-gossamer-shared/go/discovery => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/discovery

replace github.com/egidinas/loom-gossamer-shared/go/mathutil => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/mathutil

replace github.com/egidinas/loom-gossamer-shared/go/livebus => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/livebus

replace github.com/egidinas/loom-gossamer-shared/go/mesh => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/mesh

replace github.com/egidinas/loom-gossamer-shared/go/tmtc => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/tmtc

replace github.com/egidinas/loom-gossamer-shared/go/tmtclog => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/tmtclog

replace github.com/egidinas/loom-gossamer-shared/go/telemetrytiles => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/telemetrytiles

replace github.com/egidinas/loom-gossamer-shared/go/transport => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/transport

replace github.com/egidinas/loom-gossamer-shared/go/arrowutil => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/arrowutil

replace github.com/egidinas/loom-gossamer-shared/go/schema => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/schema

replace github.com/egidinas/loom-gossamer-shared/go/dbc => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/dbc

replace github.com/egidinas/loom-gossamer-shared/go/commandsession => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/commandsession

replace github.com/egidinas/loom-gossamer-shared/go/metrics => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/metrics

replace github.com/egidinas/loom-gossamer-shared/go/modulekit => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/modulekit

replace github.com/egidinas/loom-gossamer-shared/go/edgepub => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/edgepub

replace github.com/egidinas/loom-gossamer-shared/go/graphcompose => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/graphcompose

replace github.com/egidinas/loom-gossamer-shared/go/ipc => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/ipc

replace github.com/egidinas/loom-gossamer-shared/go/lifecycle => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/lifecycle

replace github.com/egidinas/loom-gossamer-shared/go/otelutil => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/otelutil

replace github.com/egidinas/loom-gossamer-shared/go/graphsem => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/graphsem

replace github.com/egidinas/loom-gossamer-shared/go/canroute => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/canroute

replace github.com/egidinas/loom-gossamer-shared/go/nodetransport => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/nodetransport

replace github.com/egidinas/loom-gossamer-shared/go/receipts => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/receipts

replace github.com/egidinas/loom-gossamer-shared/go/envelope => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/envelope

replace github.com/egidinas/loom-gossamer-shared/go/safego => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/safego

replace github.com/egidinas/loom-gossamer-shared/go/contractcheck => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/contractcheck

replace github.com/egidinas/loom-gossamer-shared/go/errorsx => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/errorsx

replace github.com/egidinas/loom-gossamer-shared/go/envutil => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/envutil

replace github.com/egidinas/loom-gossamer-shared/go/jsonfile => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/jsonfile

replace github.com/egidinas/loom-gossamer-shared/go/safepath => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/safepath

replace github.com/egidinas/loom-gossamer-shared/go/fixturefallback => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/fixturefallback

replace github.com/egidinas/loom-gossamer-shared/go/sequencer => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/sequencer

replace github.com/egidinas/loom-gossamer-shared/go/capabilitycatalog => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/capabilitycatalog

replace github.com/egidinas/loom-gossamer-shared/go/arrowtelemetry => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/arrowtelemetry

replace github.com/egidinas/loom-gossamer-shared/go/tilebundle => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/tilebundle

replace github.com/egidinas/loom-gossamer-shared/go/telemetrytiles/arrow => /home/svc_pmg_testbed_b/shared/loom-gossamer-shared/go/telemetrytiles/arrow

replace github.com/egidinas/signalforge => /home/svc_pmg_testbed_b/signalforge
