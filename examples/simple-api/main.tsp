import "@typespec/http";
import "@onkt/typespec";
import "./models.tsp";

using Http;
using examples.onksimpleapi.models;

@service
namespace examples.onksimpleapi.services;

alias ApiKey = {
  @header("X-API-Key") @format("uuid") apiKey: string;
};

/** UserService manages users in the system. */
@route("/api/v1")
interface UserService {
  /** Create a new user. */
  @post @route("/users") createUser(...ApiKey, ...CreateUserRequest): User;

  /** Get a user by ID. */
  @post @route("/users/get") getUser(...ApiKey, ...GetUserRequest): User;

  /** Login with different authentication methods. */
  @post @route("/auth/login") login(
    ...ApiKey,
    @header("X-Request-Id") @format("uuid") requestId: string,
    ...LoginRequest,
  ): LoginResponse;
}
