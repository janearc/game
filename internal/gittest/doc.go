// Package gittest is an in-memory git for tests: a go-git repository on
// memory object storage with an in-memory working tree. Mem is a
// repo.Runner, so no test starts git or touches a disk. It knows the
// commands the game packages send and refuses any other.
//
// Only tests import it, as net/http/httptest is only for tests.
package gittest
