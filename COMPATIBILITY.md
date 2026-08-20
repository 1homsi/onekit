# OneKit compatibility policy

OneKit is pre-1.0, but generated APIs still need deliberate contract changes. Treat the compiled `.onk` schema as the public contract and run `onek compat` before merging a schema change.

## Changes that require migration

The following changes are breaking for existing consumers:

- removing or renaming a message, enum, field, enum value, oneof variant, service, RPC, header, route, or stream;
- changing a field or RPC type, optional/repeated presence, map key type, JSON name, oneof tag, HTTP verb/path, route prefix, body binding, query/header binding, or declared error status;
- tightening validation, changing JSON encoding, changing a header from optional to required, or changing an authentication scheme;
- changing generated package/module paths or the output layout consumed by an application.

Use a new schema name, route, or versioned package when old and new contracts must coexist. Do not silently reuse a wire name for a different meaning.

## Changes that are normally compatible

Adding an optional field, a new RPC/route, a new service, or a new error variant is normally compatible. Adding a required request field, changing an existing default, or adding a validator to an existing field is not compatible unless every consumer is upgraded together.

## Release workflow

1. Format and validate the schema: `onek fmt --check` and `onek check`.
2. Compare the proposed schema with the last released schema: `onek compat ./previous-schema . --json`.
3. Review every finding. A non-empty compatibility result exits with status `2` and must be explicitly accepted as a versioned migration.
4. Regenerate outputs and require `make check-generated` to pass.
5. Publish the schema and generated artifacts together with the release that contains the migration.

## Legacy contracts

`AllowLegacyContracts` exists for migration of older consumers that use scalar `@required` or `@nullable` forms. New schemas should use the `?` optional marker and valid generated member names. Legacy mode must not be used to bypass a new contract review; remove it from a project after its generated consumers have migrated.

This policy will be revisited before OneKit 1.0.0. The 1.0 release will require a documented compatibility guarantee and a deprecation window for schema syntax changes.
