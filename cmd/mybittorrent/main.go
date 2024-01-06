package main

import (
	// Uncomment this line to pass the first stage
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"unicode"
	// bencode "github.com/jackpal/bencode-go" // Available if you need it!
)

func decodeString(bencodedString string) (string, int, error) {
	firstColonIndex := 0

	for bencodedString[firstColonIndex] != ':' {
		firstColonIndex++
	}

	lengthStr := bencodedString[:firstColonIndex]

	length, err := strconv.Atoi(lengthStr)
	if err != nil {
		return "", 0, err
	}
	partLength := length + firstColonIndex + 1

	return bencodedString[firstColonIndex+1 : firstColonIndex+1+length], partLength, nil
}

func decodeInteger(bencodedNumber string) (int, int, error) {
	numberEnd := 1
	for bencodedNumber[numberEnd] != 'e' {
		numberEnd++
	}
	number, err := strconv.Atoi(bencodedNumber[1:numberEnd])
	if err != nil {
		return 0, 0, err
	}

	var partLength int
	if number > 0 {
		partLength = int(math.Ceil(math.Log10(float64(number))) + 2)
	} else {
		base := math.Abs(float64(number))
		partLength = int(math.Ceil(math.Log10(base)) + 3)
	}

	return number, partLength, nil
}

func decodeList(bencodedList string) (interface{}, int, error) {
	if bencodedList == "le" {
		return []bool{}, 0, nil
	}
	var elements []interface{}
	lastElementEndIndex := 0
	for bencodedList[lastElementEndIndex+1] != 'e' {
		element, elementLength, err := decodeBencode(bencodedList[lastElementEndIndex+1:])
		if err != nil {
			return "", 0, err
		}
		elements = append(elements, element)
		lastElementEndIndex += elementLength
	}
	return elements, lastElementEndIndex + 2, nil
}

func decodeDict(bencodedDict string) (interface{}, int, error) {
	if bencodedDict == "de" {
		return map[string]bool{}, 0, nil
	}
	dict := make(map[string]interface{})
	lastElementEndIndex := 0
	for bencodedDict[lastElementEndIndex+1] != 'e' {
		key, keyLength, err := decodeString(bencodedDict[lastElementEndIndex+1:])
		if err != nil {
			return "", 0, err
		}
		value, valueLength, err := decodeBencode(bencodedDict[lastElementEndIndex+keyLength+1:])
		if err != nil {
			return "", 0, err
		}
		dict[key] = value
		lastElementEndIndex += keyLength + valueLength
	}
	return dict, lastElementEndIndex + 2, nil
}

func decodeBencode(bencodedString string) (interface{}, int, error) {
	if unicode.IsDigit(rune(bencodedString[0])) {
		return decodeString(bencodedString)
	}

	if bencodedString[0] == 'i' {
		return decodeInteger(bencodedString)
	}

	if bencodedString[0] == 'l' {
		return decodeList(bencodedString)
	}

	if bencodedString[0] == 'd' {
		return decodeDict(bencodedString)
	}

	return "", 0, fmt.Errorf("unsupported type")
}

func main() {
	command := os.Args[1]

	if command == "decode" {
		bencodedValue := os.Args[2]

		decoded, _, err := decodeBencode(bencodedValue)
		if err != nil {
			fmt.Println(err)
			return
		}

		jsonOutput, _ := json.Marshal(decoded)
		fmt.Println(string(jsonOutput))
	} else {
		fmt.Println("Unknown command: " + command)
		os.Exit(1)
	}
}
