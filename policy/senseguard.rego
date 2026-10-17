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
