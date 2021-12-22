// Copyright 2021 Coinbase, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package construction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"reflect"
	"strconv"
	"strings"

	"github.com/coinbase/rosetta-sdk-go/parser"
	"github.com/coinbase/rosetta-sdk-go/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/maticnetwork/polygon-rosetta/polygon"
	svcErrors "github.com/maticnetwork/polygon-rosetta/services/errors"
)

// ConstructionPreprocess implements the /construction/preprocess
// endpoint.
func (a *APIService) ConstructionPreprocess(
	ctx context.Context,
	request *types.ConstructionPreprocessRequest,
) (*types.ConstructionPreprocessResponse, *types.Error) {
	fromOp, toOp, err := matchTransferOperations(request.Operations)
	if err != nil {
		return nil, svcErrors.WrapErr(svcErrors.ErrUnclearIntent, err)
	}

	fromAdd := fromOp.Account.Address
	toAdd := toOp.Account.Address

	// Ensure valid from address
	checkFrom, ok := polygon.ChecksumAddress(fromAdd)
	if !ok {
		return nil, svcErrors.WrapErr(
			svcErrors.ErrInvalidAddress,
			fmt.Errorf("source address: %s is not a valid address", fromAdd),
		)
	}

	// Ensure valid to address
	checkTo, ok := polygon.ChecksumAddress(toAdd)
	if !ok {
		return nil, svcErrors.WrapErr(
			svcErrors.ErrInvalidAddress,
			fmt.Errorf("destination address: %s is not a valid address", toAdd),
		)
	}

	value := new(big.Int)
	value.SetString(toOp.Amount.Value, 10)
	preprocessOutputOptions := &options{
		From:  checkFrom,
		To:    checkTo,
		Value: value,
	}

	// Override nonce
	if v, ok := request.Metadata["nonce"]; ok {
		stringObj, ok := v.(string)
		if !ok {
			return nil, svcErrors.WrapErr(
				svcErrors.ErrInvalidNonce,
				fmt.Errorf("%s is not a valid nonce string", v),
			)
		}
		bigObj, ok := new(big.Int).SetString(stringObj, 10) //nolint:gomnd
		if !ok {
			return nil, svcErrors.WrapErr(
				svcErrors.ErrInvalidNonce,
				fmt.Errorf("%s is not a valid nonce", v),
			)
		}
		preprocessOutputOptions.Nonce = bigObj
	}

	// Only supports ERC20 transfers
	currency := fromOp.Amount.Currency
	if _, ok := request.Metadata["method_signature"]; !ok && !isNativeCurrency(currency) {
		tokenContractAddress, err := getTokenContractAddress(currency)
		if err != nil {
			return nil, svcErrors.WrapErr(svcErrors.ErrInvalidTokenContractAddress, err)
		}

		data, err := constructERC20TransferData(checkTo, value)
		if err != nil {
			return nil, svcErrors.WrapErr(svcErrors.ErrFetchFunctionSignatureMethodID, err)
		}

		preprocessOutputOptions.TokenAddress = tokenContractAddress
		preprocessOutputOptions.Data = data
		preprocessOutputOptions.Value = big.NewInt(0) // MATIC value is 0 when sending ERC20
	}

	if v, ok := request.Metadata["method_signature"]; ok {
		methodSigStringObj := v.(string)
		if !ok {
			return nil, svcErrors.WrapErr(
				svcErrors.ErrInvalidSignature,
				fmt.Errorf("%s is not a valid signature string", v),
			)
		}
		contractAddress, ok := request.Metadata["contract_address"].(string)
		if !ok {
			return nil, svcErrors.WrapErr(
				svcErrors.ErrInvalidAddress,
				fmt.Errorf("%s is not a valid string", contractAddress),
			)
		}
		var methodArgs []string
		if v, ok := request.Metadata["method_args"]; ok {
			methodArgsBytes, _ := json.Marshal(v)
			err := json.Unmarshal(methodArgsBytes, &methodArgs)
			if err != nil {
				fmt.Println("Error in unmarshal")
			}
		}
		fmt.Println(methodArgs)
		fmt.Printf("type: %T\n", methodArgs)
		data, err := constructContractCallData(methodSigStringObj, methodArgs)
		if err != nil {
			return nil, svcErrors.WrapErr(svcErrors.ErrFetchFunctionSignatureMethodID, err)
		}
		preprocessOutputOptions.TokenAddress = contractAddress
		preprocessOutputOptions.Data = data
		preprocessOutputOptions.Value = big.NewInt(0) // MATIC value is 0 in any contract call

	}

	marshaled, err := marshalJSONMap(preprocessOutputOptions)
	if err != nil {
		return nil, svcErrors.WrapErr(svcErrors.ErrUnableToParseIntermediateResult, err)
	}

	return &types.ConstructionPreprocessResponse{
		Options: marshaled,
	}, nil
}

// matchTransferOperations attempts to match a slice of operations with a `transfer`
// intent. This will match both Native token (Matic) and ERC20 tokens
func matchTransferOperations(operations []*types.Operation) (
	*types.Operation,
	*types.Operation,
	error,
) {
	descriptions := &parser.Descriptions{
		OperationDescriptions: []*parser.OperationDescription{
			{
				Type: polygon.CallOpType,
				Account: &parser.AccountDescription{
					Exists: true,
				},
				Amount: &parser.AmountDescription{
					Exists: true,
					Sign:   parser.NegativeAmountSign,
				},
			},
			{
				Type: polygon.CallOpType,
				Account: &parser.AccountDescription{
					Exists: true,
				},
				Amount: &parser.AmountDescription{
					Exists: true,
					Sign:   parser.PositiveAmountSign,
				},
			},
		},
		ErrUnmatched: true,
	}

	matches, err := parser.MatchOperations(descriptions, operations)
	if err != nil {
		return nil, nil, err
	}

	fromOp, _ := matches[0].First()
	toOp, _ := matches[1].First()

	// Manually validate currencies since we cannot rely on parser
	if fromOp.Amount.Currency == nil || toOp.Amount.Currency == nil {
		return nil, nil, errors.New("missing currency")
	}

	if !reflect.DeepEqual(fromOp.Amount.Currency, toOp.Amount.Currency) {
		return nil, nil, errors.New("from and to currencies are not equal")
	}

	return fromOp, toOp, nil
}

// isNativeCurrency checks if the currency is the native currency
func isNativeCurrency(currency *types.Currency) bool {
	if currency == nil {
		return false
	}

	return reflect.DeepEqual(currency, polygon.Currency)
}

// getTokenContractAddress retrieves and validates the contract address
func getTokenContractAddress(currency *types.Currency) (string, error) {
	v, exists := currency.Metadata[TokenContractAddressKey]
	if !exists {
		return "", errors.New("missing token contract address")
	}

	tokenContractAddress, ok := v.(string)
	if !ok {
		return "", errors.New("token contract address is not a string")
	}

	checkTokenContractAddress, ok := polygon.ChecksumAddress(tokenContractAddress)
	if !ok {
		return "", errors.New("token contract address is not a valid address")
	}

	// TODO: verify token contract address actually exist and the Symbol matches
	return checkTokenContractAddress, nil
}

// constructERC20TransferData constructs the data field of a Polygon
// transaction, including the recipient address and the amount
func constructERC20TransferData(to string, value *big.Int) ([]byte, error) {
	methodID, err := erc20TransferMethodID()
	if err != nil {
		return nil, err
	}

	var data []byte
	data = append(data, methodID...)

	toAddress := common.HexToAddress(to)
	paddedToAddress := common.LeftPadBytes(toAddress.Bytes(), 32)
	data = append(data, paddedToAddress...)

	paddedAmount := common.LeftPadBytes(value.Bytes(), 32)
	data = append(data, paddedAmount...)

	return data, nil
}

// constructContractCallData constructs the data field of a Polygon
// transaction
func constructContractCallData(methodSig string, methodArgs []string) ([]byte, error) {

	methodID, err := contractCallMethodID(methodSig)
	if err != nil {
		return nil, err
	}

	var data []byte
	data = append(data, methodID...)

	splitSigByLeadingParenthesis := strings.Split(methodSig, "(")
	if len(splitSigByLeadingParenthesis) < 2 {
		return data, nil
	}
	splitSigByTrailingParenthesis := strings.Split(splitSigByLeadingParenthesis[1], ")")
	if len(splitSigByTrailingParenthesis) < 1 {
		return data, nil
	}
	splitSigByComma := strings.Split(splitSigByTrailingParenthesis[0], ",")
	fmt.Println(splitSigByComma)

	for i, v := range splitSigByComma {
		typed, _ := abi.NewType(v, v, nil)
		arguments := abi.Arguments{
			{
				Type: typed,
			},
		}
		switch {
		case v == "address":
			{
				fmt.Println("in address case")
				value := common.HexToAddress(methodArgs[i])
				bytes, _ := arguments.Pack(
					value,
				)
				fmt.Println(bytes)
				data = append(data, bytes...)
			}
		case strings.HasPrefix(v, "uint") || strings.HasPrefix(v, "int"):
			{
				fmt.Println("in int case")
				value := new(big.Int)
				value.SetString(methodArgs[i], 10)
				bytes, _ := arguments.Pack(
					value,
				)
				fmt.Println(bytes)
				data = append(data, bytes...)
			}
		case strings.HasPrefix(v, "bytes"):
			{
				fmt.Println("in bytes case")
				value := [32]byte{}
				copy(value[:], []byte(methodArgs[i]))
				bytes, _ := arguments.Pack(
					value,
				)
				fmt.Println(bytes)
				data = append(data, bytes...)
			}
		case strings.HasPrefix(v, "string"):
			{
				bytes, _ := arguments.Pack(
					methodArgs[i],
				)
				fmt.Println(bytes)
				data = append(data, bytes...)
			}
		case strings.HasPrefix(v, "bool"):
			{
				fmt.Println("in bool case")
				value, err := strconv.ParseBool(methodArgs[i])
				if err != nil {
					log.Fatal(err)
				}
				bytes, _ := arguments.Pack(
					value,
				)
				fmt.Println("in bool", bytes)
				data = append(data, bytes...)
			}

		}

	}
	fmt.Println("final data:", data)

	// condition needs to be added splitByComma and Args length should be same

	return data, nil
}

//
