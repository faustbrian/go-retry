// Package retryhttp classifies bounded HTTP response failures and parses
// Retry-After without deciding whether an HTTP operation is safe to repeat.
// Its import path is github.com/faustbrian/go-retry/v3/adapters/http. Start with
// New for status/transport classification and NewError for immutable bounded
// response metadata. Transient callbacks are borrowed, synchronous and unsandboxed.
// No HTTP client, response body, replay key or connection lifecycle is owned.
package retryhttp
