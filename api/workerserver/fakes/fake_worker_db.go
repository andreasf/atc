// This file was generated by counterfeiter
package fakes

import (
	"sync"
	"time"

	"github.com/concourse/atc/api/workerserver"
	"github.com/concourse/atc/db"
)

type FakeWorkerDB struct {
	SaveWorkerStub        func(db.WorkerInfo, time.Duration) (db.SavedWorker, error)
	saveWorkerMutex       sync.RWMutex
	saveWorkerArgsForCall []struct {
		arg1 db.WorkerInfo
		arg2 time.Duration
	}
	saveWorkerReturns struct {
		result1 db.SavedWorker
		result2 error
	}
	WorkersStub        func() ([]db.SavedWorker, error)
	workersMutex       sync.RWMutex
	workersArgsForCall []struct{}
	workersReturns     struct {
		result1 []db.SavedWorker
		result2 error
	}
}

func (fake *FakeWorkerDB) SaveWorker(arg1 db.WorkerInfo, arg2 time.Duration) (db.SavedWorker, error) {
	fake.saveWorkerMutex.Lock()
	fake.saveWorkerArgsForCall = append(fake.saveWorkerArgsForCall, struct {
		arg1 db.WorkerInfo
		arg2 time.Duration
	}{arg1, arg2})
	fake.saveWorkerMutex.Unlock()
	if fake.SaveWorkerStub != nil {
		return fake.SaveWorkerStub(arg1, arg2)
	} else {
		return fake.saveWorkerReturns.result1, fake.saveWorkerReturns.result2
	}
}

func (fake *FakeWorkerDB) SaveWorkerCallCount() int {
	fake.saveWorkerMutex.RLock()
	defer fake.saveWorkerMutex.RUnlock()
	return len(fake.saveWorkerArgsForCall)
}

func (fake *FakeWorkerDB) SaveWorkerArgsForCall(i int) (db.WorkerInfo, time.Duration) {
	fake.saveWorkerMutex.RLock()
	defer fake.saveWorkerMutex.RUnlock()
	return fake.saveWorkerArgsForCall[i].arg1, fake.saveWorkerArgsForCall[i].arg2
}

func (fake *FakeWorkerDB) SaveWorkerReturns(result1 db.SavedWorker, result2 error) {
	fake.SaveWorkerStub = nil
	fake.saveWorkerReturns = struct {
		result1 db.SavedWorker
		result2 error
	}{result1, result2}
}

func (fake *FakeWorkerDB) Workers() ([]db.SavedWorker, error) {
	fake.workersMutex.Lock()
	fake.workersArgsForCall = append(fake.workersArgsForCall, struct{}{})
	fake.workersMutex.Unlock()
	if fake.WorkersStub != nil {
		return fake.WorkersStub()
	} else {
		return fake.workersReturns.result1, fake.workersReturns.result2
	}
}

func (fake *FakeWorkerDB) WorkersCallCount() int {
	fake.workersMutex.RLock()
	defer fake.workersMutex.RUnlock()
	return len(fake.workersArgsForCall)
}

func (fake *FakeWorkerDB) WorkersReturns(result1 []db.SavedWorker, result2 error) {
	fake.WorkersStub = nil
	fake.workersReturns = struct {
		result1 []db.SavedWorker
		result2 error
	}{result1, result2}
}

var _ workerserver.WorkerDB = new(FakeWorkerDB)
