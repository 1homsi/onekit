export function fetchRouter(
	routes: RouteDescriptor[],
): (req: Request) => Promise<Response> {
	return async (req: Request): Promise<Response> => {
		const url = new URL(req.url);
		const matching = routes.filter(
			(route) => matchPath(route.path, url.pathname) !== null,
		);
		if (matching.length === 0)
			return jsonResponse({ message: "not found" }, 404);
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
		return new Response(JSON.stringify({ message: "method not allowed" }), {
			status: 405,
			headers: { "Content-Type": "application/json", Allow: allow },
		});
	};
}
