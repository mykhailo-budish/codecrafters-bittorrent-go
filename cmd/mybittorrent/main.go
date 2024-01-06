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

func decodeList(bencodedString string) (interface{}, int, error) {
	if bencodedString == "le" {
		return []bool{}, 0, nil
	}
	var elements []interface{}
	indexBeforeListEnd, lastElementEndIndex := len(bencodedString)-1, 0
	bencodedListElements := bencodedString[1:indexBeforeListEnd]
	for lastElementEndIndex+1 != indexBeforeListEnd {
		element, elementLength, err := decodeBencode(bencodedListElements[lastElementEndIndex:])
		if err != nil {
			return "", 0, err
		}
		elements = append(elements, element)
		lastElementEndIndex += elementLength
	}
	return elements, lastElementEndIndex + 2, nil
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
