package main

// MaClaw App domain code is split across focused files (same package):
//
//	app_maclaw_app_types.go     - shared types
//	app_maclaw_app_hub.go       - Hub download / submit / sync / submission queue
//	app_maclaw_app_install.go   - install plan / registry / DataSrv registration
//	app_maclaw_app_deps.go      - skill dependency resolve / bundled install
//	app_maclaw_app_package.go   - package parse / governance / review gates
//	app_maclaw_app_approval.go  - approval workflow runtime
//	app_maclaw_app_business.go  - enterprise business operations
//	app_maclaw_app_helpers.go   - shared helpers
//	app_maclaw_app_run_evidence.go - durable run history / runtime health
//
// Keep new MaClaw App APIs in the matching file rather than re-growing this stub.
