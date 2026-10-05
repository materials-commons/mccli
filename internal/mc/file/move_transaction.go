package file

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type MoveTransaction struct {
	// Unique identifier for the transaction.
	ID string `json:"id"`

	// The working directory that the command associated with this transaction was run in.
	CommandWorkingDir string `json:"command_working_dir"`

	// The arguments that the command associated with this transaction was run with.
	CommandArgs []string `json:"command_args"`

	// Each transaction is a single item. This captures the item in the move args
	// that failed (e.g., the file or dir that was being moved).
	Source string `json:"source"`

	// The destination of the move. This could be a file or a directory. For example, you could move
	// a file to a file-destination to effectively do a rename.
	Dest string `json:"dest"`

	// This is the full local path of the source. For example, `mv file1 dir1`, since file1 is the source,
	// we'd store the full path to file1 here, e.g., /home/user/proj-name/file1
	SourceFullPath string `json:"source_local_path"`

	// This is the full local path of the destination. For example, `mv file1 dir1`, since dir1 is the
	// destination, we'd store the full path to dir1 here, e.g., /home/user/proj-name/dir1
	DestFullPath string `json:"dest_local_path"`

	// Since the move was successful on the remote, we store the remote file ID of the destination.
	RemoteDestFileID *int `json:"remote_dest_file_id"`

	// Date and Time this transaction was created
	CreatedAt time.Time `json:"created_at"`

	// Date and Time this transaction was updated. This would only happen if it failed again.
	UpdatedAt time.Time `json:"updated_at"`

	// A user-friendly error message if the transaction failed. This helps the user understand
	// why the transaction failed and what they can do to fix it.
	LastError string `json:"last_error"`
}

// MoveDB implements a store for MoveTransactions. Transactions are stored as JSON files in
// the dbPath directory. MoveDB is threadsafe, but multiple MoveDB instantiations are not
// safe to use simultaneously if they all write to the same dbPath directory.
type MoveDB struct {
	// Path to the directory where MoveTransactions are stored as JSON files.
	path string

	// Mutex to protect access to the database.
	mu sync.Mutex
}

// NewMoveDB creates a new MoveDB instance.
func NewMoveDB(dbPath string) (*MoveDB, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("dbPath cannot be empty")
	}

	if !IsDir(dbPath) {
		return nil, fmt.Errorf("dbPath must be a directory")
	}

	return &MoveDB{path: dbPath}, nil
}

// SaveTransaction saves a MoveTransaction to the database. SaveTransaction will assign
// a unique ID to the transaction if it does not already have one. SaveTransaction will
// fail if the transaction already exists in the database.
func (db *MoveDB) SaveTransaction(txn *MoveTransaction) (*MoveTransaction, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	if txn.ID == "" {
		txn.ID = uuid.New().String()
	}

	txnPath, err := db.getTransactionPath(txn.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get transaction path: %w", err)
	}

	if exists, _ := FileExists(txnPath); exists {
		return nil, fmt.Errorf("transaction %s already exists", txn.ID)
	}

	if err = db.writeTxnToFile(txn); err != nil {
		return nil, fmt.Errorf("failed to save transaction: %w", err)
	}

	return txn, nil
}

// GetTransactions returns all MoveTransactions stored in the database. Skips directories, and files don't end
// in .json. Skips json files that return a parse error.
func (db *MoveDB) GetTransactions() ([]MoveTransaction, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	var txns []MoveTransaction
	err := db.listTransactions(func(txnPath string) (bool, error) {
		txn, err := db.readTxnFromFile(txnPath)
		if err != nil {
			return true, fmt.Errorf("failed to read transaction from file: %w", err)
		}

		txns = append(txns, *txn)
		return true, nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list transactions: %w", err)
	}
	return txns, nil
}

// DeleteTransaction deletes a MoveTransaction from the database.
func (db *MoveDB) DeleteTransaction(txnID string) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	txnPath, err := db.getTransactionPath(txnID)
	if err != nil {
		return fmt.Errorf("failed to get transaction path: %w", err)
	}
	return os.Remove(txnPath)
}

// GetTransaction returns a MoveTransaction from the database.
func (db *MoveDB) GetTransaction(txnID string) (*MoveTransaction, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	return db.readTxnByIDFromFile(txnID)
}

// UpdateTransaction updates a MoveTransaction in the database. Doesn't check if the file exists, so
// if it doesn't this is equivalent to creating a new transaction.
func (db *MoveDB) UpdateTransaction(txn *MoveTransaction) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	return db.writeTxnToFile(txn)
}

// ClearAllTransactions deletes all MoveTransactions from the database.
func (db *MoveDB) ClearAllTransactions() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	return db.listTransactions(func(txnPath string) (bool, error) {
		err := os.Remove(txnPath)
		if err != nil {
			return true, fmt.Errorf("failed to delete transaction: %w", err)
		}
		return true, nil
	})
}

// getTransactionPath returns the path to a MoveTransaction file in the database. It validates that the
// txnID is a valid uuid.
func (db *MoveDB) getTransactionPath(txnID string) (string, error) {
	if err := uuid.Validate(txnID); err != nil {
		return "", err
	}
	return db.path + "/" + txnID + ".json", nil
}

// listTransactions lists all MoveTransactions in the database. It calls callback on each file. The method
// checks that the file is a JSON file. It skips files that don't end with ".json", or are directories.
// On error the callback should return true to continue iterating, and false to stop.
func (db *MoveDB) listTransactions(callback func(txnPath string) (bool, error)) error {
	files, err := os.ReadDir(db.path)
	if err != nil {
		return err
	}

	for _, f := range files {
		if f.IsDir() {
			continue
		}

		if !strings.HasSuffix(f.Name(), ".json") {
			continue
		}

		txnPath := filepath.Join(db.path, f.Name())
		shouldContinue, err := callback(txnPath)
		if err != nil {
			if shouldContinue {
				continue
			}
			return err
		}
	}

	return nil
}

// writeTxnToFile writes a MoveTransaction to the database. It validates that the txnID is a valid uuid.
func (db *MoveDB) writeTxnToFile(txn *MoveTransaction) error {
	txnPath, err := db.getTransactionPath(txn.ID)
	if err != nil {
		return fmt.Errorf("failed to get transaction path: %w", err)
	}
	b, err := json.Marshal(txn)
	if err != nil {
		return fmt.Errorf("failed to marshal transaction: %w", err)
	}
	return os.WriteFile(txnPath, b, 0644)
}

// readTxnByIDFromFile reads a MoveTransaction from the database. It validates that the txnID is a valid uuid.
func (db *MoveDB) readTxnByIDFromFile(txnID string) (*MoveTransaction, error) {
	txnPath, err := db.getTransactionPath(txnID)
	if err != nil {
		return nil, fmt.Errorf("failed to get transaction path: %w", err)
	}
	return db.readTxnFromFile(txnPath)
}

// readTxnFromFile reads a MoveTransaction from the database. It validates that the filePath is a valid file path.
func (db *MoveDB) readTxnFromFile(filePath string) (*MoveTransaction, error) {
	b, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read transaction: %w", err)
	}

	var txn MoveTransaction
	err = json.Unmarshal(b, &txn)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal transaction: %w", err)
	}
	return &txn, nil
}
