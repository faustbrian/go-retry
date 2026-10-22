// Package retrypostgres classifies PostgreSQL failures conservatively without
// deciding whether a transaction or statement is safe to repeat.
// Its import path is github.com/faustbrian/go-retry/v3/adapters/postgres. New or
// the zero Classifier is ready for concurrent use. It imports pgx error helpers
// but opens no pool or connection and starts no worker. It classifies SQLSTATE
// and transport evidence; caller cancellation wins before backend evidence.
// Statements, transactions, outcome reconciliation and replay keys stay caller-owned.
package retrypostgres
