package controllers

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/daglabs/btcd/apiserver/database"
	"github.com/daglabs/btcd/apiserver/jsonrpc"
	"github.com/daglabs/btcd/apiserver/models"
	"github.com/daglabs/btcd/apiserver/utils"
	"github.com/daglabs/btcd/btcjson"
	"github.com/daglabs/btcd/util/daghash"
	"github.com/daglabs/btcd/wire"
	"github.com/jinzhu/gorm"
)

const maxGetTransactionsLimit = 1000

// GetTransactionByIDHandler returns a transaction by a given transaction ID.
func GetTransactionByIDHandler(txID string) (interface{}, *utils.HandlerError) {
	if bytes, err := hex.DecodeString(txID); err != nil || len(bytes) != daghash.TxIDSize {
		return nil, utils.NewHandlerError(http.StatusUnprocessableEntity,
			fmt.Sprintf("The given txid is not a hex-encoded %d-byte hash.", daghash.TxIDSize))
	}

	db, err := database.DB()
	if err != nil {
		return nil, utils.NewInternalServerHandlerError(err.Error())
	}

	tx := &models.Transaction{}
	query := db.Where(&models.Transaction{TransactionID: txID})
	dbResult := addTxPreloadedFields(query).First(&tx)
	if dbResult.RecordNotFound() && len(dbResult.GetErrors()) == 1 {
		return nil, utils.NewHandlerError(http.StatusNotFound, "No transaction with the given txid was found.")
	}
	if len(dbResult.GetErrors()) > 0 {
		return nil, utils.NewHandlerErrorFromDBErrors("Some errors where encountered when loading transaction from the database:", dbResult.GetErrors())
	}
	return convertTxModelToTxResponse(tx), nil
}

// GetTransactionByHashHandler returns a transaction by a given transaction hash.
func GetTransactionByHashHandler(txHash string) (interface{}, *utils.HandlerError) {
	if bytes, err := hex.DecodeString(txHash); err != nil || len(bytes) != daghash.HashSize {
		return nil, utils.NewHandlerError(http.StatusUnprocessableEntity,
			fmt.Sprintf("The given txhash is not a hex-encoded %d-byte hash.", daghash.HashSize))
	}

	db, err := database.DB()
	if err != nil {
		return nil, utils.NewHandlerError(http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))
	}

	tx := &models.Transaction{}
	query := db.Where(&models.Transaction{TransactionHash: txHash})
	dbResult := addTxPreloadedFields(query).First(&tx)
	if dbResult.RecordNotFound() && len(dbResult.GetErrors()) == 1 {
		return nil, utils.NewHandlerError(http.StatusNotFound, "No transaction with the given txhash was found.")
	}
	if len(dbResult.GetErrors()) > 0 {
		return nil, utils.NewHandlerErrorFromDBErrors("Some errors where encountered when loading transaction from the database:", dbResult.GetErrors())
	}
	return convertTxModelToTxResponse(tx), nil
}

// GetTransactionsByAddressHandler searches for all transactions
// where the given address is either an input or an output.
func GetTransactionsByAddressHandler(address string, skip uint64, limit uint64) (interface{}, *utils.HandlerError) {
	if limit > maxGetTransactionsLimit {
		return nil, utils.NewHandlerError(http.StatusUnprocessableEntity,
			fmt.Sprintf("The maximum allowed value for the limit is %d", maxGetTransactionsLimit))
	}

	db, err := database.DB()
	if err != nil {
		return nil, utils.NewHandlerError(http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))
	}

	txs := []*models.Transaction{}
	query := db.
		Joins("LEFT JOIN `transaction_outputs` ON `transaction_outputs`.`transaction_id` = `transactions`.`id`").
		Joins("LEFT JOIN `addresses` AS `out_addresses` ON `out_addresses`.`id` = `transaction_outputs`.`address_id`").
		Joins("LEFT JOIN `transaction_inputs` ON `transaction_inputs`.`transaction_id` = `transactions`.`id`").
		Joins("LEFT JOIN `transaction_outputs` AS `inputs_outs` ON `inputs_outs`.`id` = `transaction_inputs`.`transaction_output_id`").
		Joins("LEFT JOIN `addresses` AS `in_addresses` ON `in_addresses`.`id` = `inputs_outs`.`address_id`").
		Where("`out_addresses`.`address` = ?", address).
		Or("`in_addresses`.`address` = ?", address).
		Limit(limit).
		Offset(skip).
		Order("`transactions`.`id` ASC")
	dbErrors := addTxPreloadedFields(query).Find(&txs).GetErrors()
	if len(dbErrors) > 0 {
		return nil, utils.NewHandlerErrorFromDBErrors("Some errors where encountered when loading transactions from the database:", dbErrors)
	}
	txResponses := make([]*transactionResponse, len(txs))
	for i, tx := range txs {
		txResponses[i] = convertTxModelToTxResponse(tx)
	}
	return txResponses, nil
}

// GetUTXOsByAddressHandler searches for all UTXOs that belong to a certain address.
func GetUTXOsByAddressHandler(address string) (interface{}, *utils.HandlerError) {
	db, err := database.DB()
	if err != nil {
		return nil, utils.NewHandlerError(http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))
	}

	var transactionOutputs []*models.TransactionOutput
	dbErrors := db.
		Joins("LEFT JOIN `addresses` ON `addresses`.`id` = `transaction_outputs`.`address_id`").
		Where("`addresses`.`address` = ? AND `transaction_outputs`.`is_spent` = 0", address).
		Preload("Transaction.AcceptingBlock").
		Find(&transactionOutputs).GetErrors()
	if len(dbErrors) > 0 {
		return nil, utils.NewHandlerErrorFromDBErrors("Some errors where encountered when loading UTXOs from the database:", dbErrors)
	}

	UTXOsResponses := make([]*transactionOutputResponse, len(transactionOutputs))
	for i, transactionOutput := range transactionOutputs {
		UTXOsResponses[i] = &transactionOutputResponse{
			Value:                   transactionOutput.Value,
			ScriptPubKey:            hex.EncodeToString(transactionOutput.ScriptPubKey),
			AcceptingBlockHash:      transactionOutput.Transaction.AcceptingBlock.BlockHash,
			AcceptingBlockBlueScore: transactionOutput.Transaction.AcceptingBlock.BlueScore,
		}
	}
	return UTXOsResponses, nil
}

func addTxPreloadedFields(query *gorm.DB) *gorm.DB {
	return query.Preload("AcceptingBlock").
		Preload("Subnetwork").
		Preload("TransactionOutputs").
		Preload("TransactionOutputs.Address").
		Preload("TransactionInputs.TransactionOutput.Transaction").
		Preload("TransactionInputs.TransactionOutput.Address")
}

// PostTransaction forwards a raw transaction to the JSON-RPC API server
func PostTransaction(requestBody []byte) *utils.HandlerError {
	client, err := jsonrpc.GetClient()
	if err != nil {
		return utils.NewInternalServerHandlerError(err.Error())
	}

	rawTx := &RawTransaction{}
	err = json.Unmarshal(requestBody, rawTx)
	if err != nil {
		return utils.NewHandlerErrorWithCustomClientMessage(http.StatusUnprocessableEntity,
			fmt.Sprintf("Error unmarshalling request body: %s", err),
			"The request body is not json-formatted")
	}

	txBytes, err := hex.DecodeString(rawTx.RawTransaction)
	if err != nil {
		return utils.NewHandlerErrorWithCustomClientMessage(http.StatusUnprocessableEntity,
			fmt.Sprintf("Error decoding hex raw transaction: %s", err),
			"The raw transaction is not a hex-encoded transaction")
	}

	txReader := bytes.NewReader(txBytes)
	tx := &wire.MsgTx{}
	err = tx.BtcDecode(txReader, 0)
	if err != nil {
		return utils.NewHandlerErrorWithCustomClientMessage(http.StatusUnprocessableEntity,
			fmt.Sprintf("Error decoding raw transaction: %s", err),
			"Error decoding raw transaction")
	}

	_, err = client.SendRawTransaction(tx, true)
	if err != nil {
		if rpcErr, ok := err.(btcjson.RPCError); ok && rpcErr.Code == btcjson.ErrRPCVerify {
			return utils.NewHandlerError(http.StatusInternalServerError, rpcErr.Message)
		}
		return utils.NewHandlerError(http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))
	}

	return nil
}