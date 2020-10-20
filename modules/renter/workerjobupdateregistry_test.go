package renter

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"gitlab.com/NebulousLabs/Sia/crypto"
	"gitlab.com/NebulousLabs/Sia/modules"
	"gitlab.com/NebulousLabs/Sia/modules/host/registry"
	"gitlab.com/NebulousLabs/Sia/siatest/dependencies"
	"gitlab.com/NebulousLabs/Sia/types"
	"gitlab.com/NebulousLabs/fastrand"
)

// TestUpdateRegistryJob tests the various cases of running an UpdateRegistry
// job on a host.
func TestUpdateRegistryJob(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	deps := dependencies.NewDependencyCorruptMDMOutput()
	wt, err := newWorkerTesterCustomDependency(t.Name(), modules.ProdDependencies, deps)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := wt.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	// Create a registry value.
	sk, pk := crypto.GenerateKeyPair()
	var tweak crypto.Hash
	fastrand.Read(tweak[:])
	data := fastrand.Bytes(modules.RegistryDataSize)
	rev := fastrand.Uint64n(1000) + 1
	spk := types.SiaPublicKey{
		Algorithm: types.SignatureEd25519,
		Key:       pk[:],
	}
	rv := modules.NewRegistryValue(tweak, data, rev).Sign(sk)

	// Run the UpdateRegistryJob.
	err = wt.UpdateRegistry(context.Background(), spk, rv)
	if err != nil {
		t.Fatal(err)
	}

	// Manually try to read the entry from the host.
	lookedUpRV, err := lookupRegistry(wt.worker, spk, tweak)
	if err != nil {
		t.Fatal(err)
	}

	// The entries should match.
	if !reflect.DeepEqual(lookedUpRV, rv) {
		t.Fatal("entries don't match")
	}

	// Run the UpdateRegistryJob again. This time it should fail with an error
	// indicating that the revision number already exists.
	err = wt.UpdateRegistry(context.Background(), spk, rv)
	if err == nil || !strings.Contains(err.Error(), registry.ErrSameRevNum.Error()) {
		t.Fatal(err)
	}

	// Make sure there is no recent error or cooldown.
	wt.staticJobUpdateRegistryQueue.mu.Lock()
	if wt.staticJobUpdateRegistryQueue.recentErr != nil {
		t.Fatal("recentErr is set", wt.staticJobUpdateRegistryQueue.recentErr)
	}
	if wt.staticJobUpdateRegistryQueue.cooldownUntil != (time.Time{}) {
		t.Fatal("cooldownUntil is set", wt.staticJobUpdateRegistryQueue.cooldownUntil)
	}
	wt.staticJobUpdateRegistryQueue.mu.Unlock()

	// Same thing again but corrupt the output.
	deps.Fail()
	err = wt.UpdateRegistry(context.Background(), spk, rv)
	deps.Disable()
	if !strings.Contains(err.Error(), crypto.ErrInvalidSignature.Error()) {
		t.Fatal(err)
	}

	// Make sure the recent error is an invalid signature error and reset the
	// cooldown.
	wt.staticJobUpdateRegistryQueue.mu.Lock()
	if !strings.Contains(wt.staticJobUpdateRegistryQueue.recentErr.Error(), crypto.ErrInvalidSignature.Error()) {
		t.Fatal(err)
	}
	if wt.staticJobUpdateRegistryQueue.cooldownUntil == (time.Time{}) {
		t.Fatal("coolDown not set")
	}
	wt.staticJobUpdateRegistryQueue.cooldownUntil = time.Time{}
	wt.staticJobUpdateRegistryQueue.recentErr = nil
	wt.staticJobUpdateRegistryQueue.mu.Unlock()

	// Run the UpdateRegistryJob with a lower revision number. This time it
	// should fail with an error indicating that the revision number already
	// exists.
	rvLowRevNum := rv
	rvLowRevNum.Revision--
	rvLowRevNum = rvLowRevNum.Sign(sk)
	err = wt.UpdateRegistry(context.Background(), spk, rvLowRevNum)
	if err == nil || !strings.Contains(err.Error(), registry.ErrLowerRevNum.Error()) {
		t.Fatal(err)
	}

	// Make sure there is no recent error or cooldown.
	wt.staticJobUpdateRegistryQueue.mu.Lock()
	if wt.staticJobUpdateRegistryQueue.recentErr != nil {
		t.Fatal("recentErr is set", wt.staticJobUpdateRegistryQueue.recentErr)
	}
	if wt.staticJobUpdateRegistryQueue.cooldownUntil != (time.Time{}) {
		t.Fatal("cooldownUntil is set", wt.staticJobUpdateRegistryQueue.cooldownUntil)
	}
	wt.staticJobUpdateRegistryQueue.mu.Unlock()

	// Same thing again but corrupt the output.
	deps.Fail()
	err = wt.UpdateRegistry(context.Background(), spk, rvLowRevNum)
	deps.Disable()
	if !strings.Contains(err.Error(), crypto.ErrInvalidSignature.Error()) {
		t.Fatal(err)
	}

	// Make sure the recent error is an invalid signature error and reset the
	// cooldown.
	wt.staticJobUpdateRegistryQueue.mu.Lock()
	if !strings.Contains(wt.staticJobUpdateRegistryQueue.recentErr.Error(), crypto.ErrInvalidSignature.Error()) {
		t.Fatal(err)
	}
	if wt.staticJobUpdateRegistryQueue.cooldownUntil == (time.Time{}) {
		t.Fatal("coolDown not set")
	}
	wt.staticJobUpdateRegistryQueue.cooldownUntil = time.Time{}
	wt.staticJobUpdateRegistryQueue.recentErr = nil
	wt.staticJobUpdateRegistryQueue.mu.Unlock()

	// Manually try to read the entry from the host.
	lookedUpRV, err = lookupRegistry(wt.worker, spk, tweak)
	if err != nil {
		t.Fatal(err)
	}

	// The entries should match.
	if !reflect.DeepEqual(lookedUpRV, rv) {
		t.Fatal("entries don't match")
	}

	// Increment the revision number and do it one more time.
	rv.Revision++
	rv = rv.Sign(sk)
	err = wt.UpdateRegistry(context.Background(), spk, rv)
	if err != nil {
		t.Fatal(err)
	}

	// Manually try to read the entry from the host.
	lookedUpRV, err = lookupRegistry(wt.worker, spk, tweak)
	if err != nil {
		t.Fatal(err)
	}

	// The entries should match.
	if !reflect.DeepEqual(lookedUpRV, rv) {
		t.Fatal("entries don't match")
	}
}
