// Package transactiontest provides an in-memory
// [github.com/a-novel-kit/golib/transaction.Transactor] for unit tests.
//
// A test substitutes *transactiontest.Transactor wherever a transaction.Transactor is expected,
// so an operation under test runs its body without a database while the test can still assert that
// the body ran inside a transaction at all.
//
// Nothing is rolled back: an in-memory double cannot undo what the callback did to the fakes
// around it. It verifies that the caller opened a scope and propagated the callback's outcome. A
// test that has a database proves a rollback discards writes.
package transactiontest
