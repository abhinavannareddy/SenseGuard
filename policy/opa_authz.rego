package system.authz

import rego.v1

# Require the expected mTLS client identity and expose only the policy decision
# and the health endpoint used by SenseGuard.
default allow := false

allow if {
	input.identity == "CN=senseguard-api"
	input.method == "POST"
	input.path == ["v1", "data", "senseguard", "allow"]
}

allow if {
	input.identity == "CN=senseguard-api"
	input.method == "GET"
	input.path == ["health"]
}
