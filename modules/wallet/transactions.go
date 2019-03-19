package wallet

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"

	"gitlab.com/NebulousLabs/Sia/build"
	"gitlab.com/NebulousLabs/Sia/encoding"
	"gitlab.com/NebulousLabs/Sia/modules"
	"gitlab.com/NebulousLabs/Sia/types"
)

var (
	errOutOfBounds = errors.New("requesting transactions at unknown confirmation heights")
)

// AddressTransactions returns all of the wallet transactions associated with a
// single unlock hash.
func (w *Wallet) AddressTransactions(uh types.UnlockHash) (pts []modules.ProcessedTransaction, err error) {
	if err := w.tg.Add(); err != nil {
		return []modules.ProcessedTransaction{}, err
	}
	defer w.tg.Done()
	// ensure durability of reported transactions
	w.mu.Lock()
	defer w.mu.Unlock()
	if err = w.syncDB(); err != nil {
		return
	}

	txnIndices, _ := dbGetAddrTransactions(w.dbTx, uh)
	for _, i := range txnIndices {
		pt, err := dbGetProcessedTransaction(w.dbTx, i)
		if err != nil {
			continue
		}
		pts = append(pts, pt)
	}
	return pts, nil
}

// AddressUnconfirmedTransactions returns all of the unconfirmed wallet transactions
// related to a specific address.
func (w *Wallet) AddressUnconfirmedTransactions(uh types.UnlockHash) (pts []modules.ProcessedTransaction, err error) {
	if err := w.tg.Add(); err != nil {
		return []modules.ProcessedTransaction{}, err
	}
	defer w.tg.Done()
	// ensure durability of reported transactions
	w.mu.Lock()
	defer w.mu.Unlock()
	if err = w.syncDB(); err != nil {
		return
	}

	// Scan the full list of unconfirmed transactions to see if there are any
	// related transactions.
	for _, pt := range w.unconfirmedProcessedTransactions {
		relevant := false
		for _, input := range pt.Inputs {
			if input.RelatedAddress == uh {
				relevant = true
				break
			}
		}
		for _, output := range pt.Outputs {
			if output.RelatedAddress == uh {
				relevant = true
				break
			}
		}
		if relevant {
			pts = append(pts, pt)
		}
	}
	return pts, err
}

// Transaction returns the transaction with the given id. 'False' is returned
// if the transaction does not exist.
func (w *Wallet) Transaction(txid types.TransactionID) (pt modules.ProcessedTransaction, found bool, err error) {
	if err := w.tg.Add(); err != nil {
		return modules.ProcessedTransaction{}, false, err
	}
	defer w.tg.Done()
	// ensure durability of reported transaction
	w.mu.Lock()
	defer w.mu.Unlock()
	if err = w.syncDB(); err != nil {
		return
	}

	// Get the keyBytes for the given txid
	keyBytes, err := dbGetTransactionIndex(w.dbTx, txid)
	if err != nil {
		for _, txn := range w.unconfirmedProcessedTransactions {
			if txn.TransactionID == txid {
				return txn, true, nil
			}
		}
		return modules.ProcessedTransaction{}, false, nil
	}

	// Retrieve the transaction
	found = encoding.Unmarshal(w.dbTx.Bucket(bucketProcessedTransactions).Get(keyBytes), &pt) == nil
	return
}

// Transactions returns all transactions relevant to the wallet that were
// confirmed in the range [startHeight, endHeight].
func (w *Wallet) Transactions(startHeight, endHeight types.BlockHeight) (sts []modules.SuperTransaction, err error) {
	if err := w.tg.Add(); err != nil {
		return nil, err
	}
	defer w.tg.Done()
	// ensure durability of reported transactions
	w.mu.Lock()
	defer w.mu.Unlock()
	if err = w.syncDB(); err != nil {
		return
	}

	height, err := dbGetConsensusHeight(w.dbTx)
	if err != nil {
		return
	} else if startHeight > height || startHeight > endHeight {
		return nil, errOutOfBounds
	}

	// Get the bucket, the largest key in it and the cursor
	bucket := w.dbTx.Bucket(bucketProcessedTransactions)
	cursor := bucket.Cursor()
	nextKey := bucket.Sequence() + 1

	// Database is empty
	if nextKey == 1 {
		return
	}

	var pt modules.ProcessedTransaction
	keyBytes := make([]byte, 8)
	var result int
	func() {
		// Recover from possible panic during binary search
		defer func() {
			r := recover()
			if r != nil {
				err = fmt.Errorf("%v", r)
			}
		}()

		// Start binary searching
		result = sort.Search(int(nextKey), func(i int) bool {
			// Create the key for the index
			binary.BigEndian.PutUint64(keyBytes, uint64(i))

			// Retrieve the processed transaction
			key, ptBytes := cursor.Seek(keyBytes)
			if build.DEBUG && key == nil {
				panic("Failed to retrieve processed Transaction by key")
			}

			// Decode the transaction
			if err = decodeProcessedTransaction(ptBytes, &pt); build.DEBUG && err != nil {
				panic(err)
			}

			return pt.ConfirmationHeight >= startHeight
		})
	}()
	if err != nil {
		return
	}

	if uint64(result) == nextKey {
		// No transaction was found
		return
	}

	// Create the key that corresponds to the result of the search
	binary.BigEndian.PutUint64(keyBytes, uint64(result))

	// Get the processed transaction and decode it
	key, ptBytes := cursor.Seek(keyBytes)
	if build.DEBUG && key == nil {
		build.Critical("Couldn't find the processed transaction from the search.")
	}
	if err = decodeProcessedTransaction(ptBytes, &pt); build.DEBUG && err != nil {
		build.Critical(err)
	}

	// Gather all transactions until endHeight is reached
	var pts []modules.ProcessedTransaction
	for pt.ConfirmationHeight <= endHeight {
		if build.DEBUG && pt.ConfirmationHeight < startHeight {
			build.Critical("wallet processed transactions are not sorted")
		}
		pts = append(pts, pt)

		// Get next processed transaction
		key, ptBytes := cursor.Next()
		if key == nil {
			break
		}

		// Decode the transaction
		if err := decodeProcessedTransaction(ptBytes, &pt); build.DEBUG && err != nil {
			panic("Failed to decode the processed transaction")
		}
	}

	// Loop over all transactions and map the id of each contract to the most
	// recent revision of this contract that has passed the maturity height.
	//revisionMap := make(map[types.FileContractID]uint64)
	//for _, pt := range pts {
	//	for _, rev := range pt.Transaction.FileContractRevisions {
	//		if height > rev.NewWindowEnd+types.MaturityDelay {
	//			revisionMap[rev.ParentID] = rev.NewRevisionNumber
	//		}
	//	}
	//}

	// Loop over all the transactions again and set the value to all contracts
	// and revisions to 0 except for the latest revision.
	sts = make([]modules.SuperTransaction, 0, len(pts))
	for _, pt := range pts {
		sts = append(sts, w.newSuperTransaction(pt))
	}
	return
}

// newSuperTransaction creates a new SuperTransaction from a
// ProcessedTransaction.
func (w *Wallet) newSuperTransaction(pt modules.ProcessedTransaction) modules.SuperTransaction {
	// Determine the value of the transaction assuming that it's a regular
	// transaction.
	var outgoingSiacoins types.Currency
	for _, input := range pt.Inputs {
		if input.FundType == types.SpecifierSiacoinInput && input.WalletAddress {
			outgoingSiacoins = outgoingSiacoins.Add(input.Value)
		}
	}
	var incomingSiacoins types.Currency
	for _, output := range pt.Outputs {
		if output.FundType == types.SpecifierMinerPayout && output.WalletAddress {
			incomingSiacoins = incomingSiacoins.Add(output.Value)
		}
		if output.FundType == types.SpecifierSiacoinOutput && output.WalletAddress {
			incomingSiacoins = incomingSiacoins.Add(output.Value)
		}
	}
	// Create the txn assuming that it's a regular txn without contracts or
	// revisions.
	st := modules.SuperTransaction{
		ProcessedTransaction:   pt,
		ConfirmedIncomingValue: incomingSiacoins,
		ConfirmedOutgoingValue: outgoingSiacoins,
	}
	// If the transaction doesn't contain contracts or revisions we are done.
	if len(pt.Transaction.FileContracts) == 0 && len(pt.Transaction.FileContractRevisions) == 0 {
		return st
	}
	// If there are contracts, then there can't be revisions. A contract
	// revision itself doesn't have a value.
	if len(pt.Transaction.FileContracts) > 0 {
		st.ConfirmedIncomingValue = types.ZeroCurrency
		//st.ConfirmedOutgoingValue = types.ZeroCurrency TODO: set outgoing
		//value also to zero if revision exists.
		return st
	}
	// Else the contract contains a revision.
	st.ConfirmedIncomingValue = types.ZeroCurrency
	st.ConfirmedOutgoingValue = types.ZeroCurrency
	panic("TODO: handle revisions")
}

// UnconfirmedTransactions returns the set of unconfirmed transactions that are
// relevant to the wallet.
func (w *Wallet) UnconfirmedTransactions() ([]modules.ProcessedTransaction, error) {
	if err := w.tg.Add(); err != nil {
		return nil, err
	}
	defer w.tg.Done()
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.unconfirmedProcessedTransactions, nil
}
