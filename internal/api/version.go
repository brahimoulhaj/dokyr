package api

// APIVersion is the version of the HTTP API, reported as "api_version" by GET /api/meta.
// It is a number of its own, not the release number, so a client (the remote CLI, a CI
// script) can ask "does this panel speak the API I was written for?" without knowing
// which release changed what.
//
// It is raised only by a breaking change: an endpoint removed or moved, a field removed
// or renamed, the type or the meaning of a field changed, a request that was accepted
// now refused, or a permission tightened for API tokens. Adding an endpoint, an optional
// request field or a response field does not raise it: clients must ignore fields they
// do not know.
//
// 1 is the API as of v1.0.0. While the releases are 0.x, breaking changes may still
// land without raising it; from 1.0.0 on they raise it, and the release notes say so.
const APIVersion = 1
