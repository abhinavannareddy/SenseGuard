package senseguard

import rego.v1

default allow := false

# Demo allow-list. In production, consumer identity must come from authenticated
# credentials, not a caller-controlled header.
allow if {
	input.consumer == "researcher"
	input.purpose == "crowd-analysis"
	input.mode in {"coarse", "noisy", "aggregate"}
}

allow if {
	input.consumer == "mall-manager"
	input.purpose == "occupancy-monitoring"
	input.mode == "aggregate"
}

# Readiness check used by the OPA container probe. It requires both a healthy
# local HTTP listener and the expected allow-list decision.
healthcheck if {
	response := http.send({
		"url": "http://127.0.0.1:8282/health",
		"method": "get",
		"timeout": "1s",
	})
	response.status_code == 200
	allow with input as {"consumer": "researcher", "purpose": "crowd-analysis", "mode": "coarse"}
}
