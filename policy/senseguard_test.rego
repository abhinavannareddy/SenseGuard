package senseguard

import rego.v1

test_researcher_can_use_crowd_analysis_modes if {
	allow with input as {"consumer": "researcher", "purpose": "crowd-analysis", "mode": "coarse"}
}
test_researcher_can_use_noisy_mode if {
	allow with input as {"consumer": "researcher", "purpose": "crowd-analysis", "mode": "noisy"}
}

test_researcher_can_use_aggregate_mode if {
	allow with input as {"consumer": "researcher", "purpose": "crowd-analysis", "mode": "aggregate"}
}

test_researcher_cannot_track_people if {
	not allow with input as {"consumer": "researcher", "purpose": "track-person", "mode": "coarse"}
}

test_researcher_cannot_use_unknown_mode if {
	not allow with input as {"consumer": "researcher", "purpose": "crowd-analysis", "mode": "raw"}
}

test_mall_manager_can_get_occupancy_aggregate if {
	allow with input as {"consumer": "mall-manager", "purpose": "occupancy-monitoring", "mode": "aggregate"}
}

test_mall_manager_cannot_get_individual_locations if {
	not allow with input as {"consumer": "mall-manager", "purpose": "occupancy-monitoring", "mode": "coarse"}
}

test_unknown_or_missing_consumer_is_denied if {
	not allow with input as {"consumer": "unknown", "purpose": "crowd-analysis", "mode": "coarse"}
	not allow with input as {"purpose": "crowd-analysis", "mode": "coarse"}
}

test_missing_purpose_and_mode_are_denied if {
	not allow with input as {"consumer": "researcher", "mode": "coarse"}
	not allow with input as {"consumer": "researcher", "purpose": "crowd-analysis"}
}
test_mall_manager_cannot_use_noisy_mode if {
	not allow with input as {"consumer": "mall-manager", "purpose": "occupancy-monitoring", "mode": "noisy"}
}

test_invalid_input_types_are_denied if {
	not allow with input as {"consumer": 123, "purpose": "crowd-analysis", "mode": "coarse"}
	not allow with input as {"consumer": "researcher", "purpose": 123, "mode": "coarse"}
	not allow with input as {"consumer": "researcher", "purpose": "crowd-analysis", "mode": 123}
}
