package perms

import "gopkg.in/macaroon-bakery.v2/bakery"

// RequiredPermissions is a map of all faraday RPC methods and their
// required macaroon permissions to access faraday.
var RequiredPermissions = map[string][]bakery.Op{
	"/frdrpc.FaradayServer/OutlierRecommendations": {{
		Entity: "recommendation",
		Action: "read",
	}},
	"/frdrpc.FaradayServer/ThresholdRecommendations": {{
		Entity: "recommendation",
		Action: "read",
	}},
	"/frdrpc.FaradayServer/RevenueReport": {{
		Entity: "report",
		Action: "read",
	}},
	"/frdrpc.FaradayServer/ChannelInsights": {{
		Entity: "insights",
		Action: "read",
	}},
	"/frdrpc.FaradayServer/ExchangeRate": {{
		Entity: "rates",
		Action: "read",
	}},
	"/frdrpc.FaradayServer/NodeAudit": {{
		Entity: "audit",
		Action: "read",
	}},
	"/frdrpc.FaradayServer/CloseReport": {{
		Entity: "report",
		Action: "read",
	}},
}
