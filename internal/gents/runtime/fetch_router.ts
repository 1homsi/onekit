export function fetchRouter(
	routes: RouteDescriptor[],
): (req: Request) => Promise<Response> {
	return async (req: Request): Promise<Response> => {
		const url = new URL(req.url);
		const matching = wildcardLast(routes).filter(
			(route) => matchPath(route.path, url.pathname) !== null,
		);
		if (matching.length === 0) return registeredError(404, "not_found", "not found");
		const head = req.method === "HEAD";
		const route = matching.find(
			(candidate) => candidate.method === req.method || (head && candidate.method === "GET"),
		);
		if (route) {
			const response = await route.handler(req);
			return head ? new Response(null, { status: response.status, statusText: response.statusText, headers: response.headers }) : response;
		}
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
