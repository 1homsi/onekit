export function fetchRouter(
	routes: RouteDescriptor[],
): (req: Request) => Promise<Response> {
	return async (req: Request): Promise<Response> => {
		const url = new URL(req.url);
		const matching = wildcardLast(routes).filter(
			(route) => matchPath(route.path, url.pathname) !== null,
		);
		if (matching.length === 0) return registeredError(404, "not_found", "not found");
		const route = matching.find(
			(candidate) => candidate.method === req.method,
		);
		if (route) return route.handler(req);
		const allow = [
			...new Set(matching.map((candidate) => candidate.method)),
		].join(", ");
		if (req.method === "OPTIONS")
			return new Response(null, {
				status: 204,
				headers: { Allow: allow },
			});
		const response = registeredError(
			405,
			"method_not_allowed",
			"method not allowed",
		);
		response.headers.set("Allow", allow);
		return response;
	};
}
