package miner

import (
	"math/rand" // We should probably switch to crypto/rand, but we should use benchmarks first.
	"time"

	"github.com/NebulousLabs/Sia/consensus"
)

// Creates a block that is ready for nonce grinding.
func (m *Miner) blockForWork() (b consensus.Block) {
	// Check for updates from the state.
	m.update()

	// Fill out the block with potentially ready values.
	b = consensus.Block{
		ParentBlockID: m.parent,
		Timestamp:     consensus.Timestamp(time.Now().Unix()),
		Nonce:         uint64(rand.Int()),
		MinerAddress:  m.address,
		Transactions:  m.transactions,
	}

	// If we've got a time earlier than the earliest legal timestamp, set the
	// timestamp equal to the earliest legal timestamp.
	if b.Timestamp < m.earliestTimestamp {
		b.Timestamp = m.earliestTimestamp

		// TODO: Add a single transaction that's just arbitrary data - a bunch
		// of randomly generated arbitrary data. This will provide entropy to
		// the block even though the timestamp isn't changing at all.
	}

	return
}

// mine attempts to generate blocks, and will run until desiredThreads is
// changd to be lower than `myThread`, which is set at the beginning of the
// function.
//
// The threading is fragile. Edit with caution!
func (m *Miner) threadedMine() {
	// Increment the number of threads running, because this thread is spinning
	// up. Also grab a number that will tell us when to shut down.
	m.mu.Lock()
	m.runningThreads++
	myThread := m.runningThreads
	m.mu.Unlock()

	// Try to solve a block repeatedly.
	for {
		// Grab the number of threads that are supposed to be running.
		m.mu.RLock()
		desiredThreads := m.desiredThreads
		m.mu.RUnlock()

		// If we are allowed to be running, mine a block, otherwise shut down.
		if desiredThreads >= myThread {
			// Grab the necessary variables for mining, and then attempt to
			// mine a block.
			m.mu.RLock()
			bfw := m.blockForWork()
			target := m.target
			iterations := m.iterationsPerAttempt
			m.mu.RUnlock()
			m.solveBlock(bfw, target, iterations)
		} else {
			m.mu.Lock()
			// Need to check the mining status again, something might have
			// changed while waiting for the lock.
			if desiredThreads < myThread {
				m.runningThreads--
				m.mu.Unlock()
				return
			}
			m.mu.Unlock()
		}
	}
}

// solveBlock takes a block, target, and number of iterations as input and
// tries to find a block that meets the target. This function can take a long
// time to complete, and should not be called with a lock.
func (m *Miner) solveBlock(blockForWork consensus.Block, target consensus.Target, iterations uint64) (b consensus.Block, solved bool, err error) {
	// solveBlock could operate on a pointer, but it's not strictly necessary
	// and it makes calling weirder/more opaque.
	b = blockForWork

	// Iterate through a bunch of nonces (from a random starting point) and try
	// to find a winnning solution.
	for maxNonce := b.Nonce + iterations; b.Nonce != maxNonce; b.Nonce++ {
		if b.CheckTarget(target) {
			// TODO: If debug, check the error value of AcceptBlock and panic
			// for err != nil.
			m.state.AcceptBlock(b)

			solved = true

			// Grab a new address for the miner.
			m.mu.Lock()
			var addr consensus.CoinAddress
			addr, _, err = m.wallet.CoinAddress()
			if err == nil { // Special case: we only update the address if there was no error while generating one.
				m.address = addr
			}
			m.mu.Unlock()
			return
		}
	}

	return
}

// SolveBlock is an exported function which will attempt to solve a block to
// add to the state. SolveBlock is less efficient than StartMining(), but is
// guaranteed to solve at most one block (useful for testing).
//
// solveBlock is both blocking and takes a long time to complete, therefore
// needs to be called without the miner being locked. For this reason,
// SolveBlock breaks typical mutex conventions and unlocks before returning.
func (m *Miner) SolveBlock() (consensus.Block, bool, error) {
	m.mu.Lock()
	m.update()
	bfw := m.blockForWork()
	target := m.target
	iterations := m.iterationsPerAttempt
	m.mu.Unlock()

	return m.solveBlock(bfw, target, iterations)
}

// StartMining spawns a bunch of mining threads which will mine until stop is
// called.
func (m *Miner) StartMining() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Increase the number of threads to m.desiredThreads.
	m.desiredThreads = m.threads
	for i := m.runningThreads; i < m.desiredThreads; i++ {
		go m.threadedMine()
	}

	return nil
}

// StopMining sets desiredThreads to 0, a value which is polled by mining
// threads. When set to 0, the mining threads will all cease mining.
func (m *Miner) StopMining() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Set desiredThreads to 0. The miners will shut down automatically.
	m.desiredThreads = 0
	return nil
}
