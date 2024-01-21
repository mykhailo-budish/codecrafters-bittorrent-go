package main

import (
	"crypto/sha1"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"unicode"
)

func _decodeString(bencodedString string) (string, int, error) {
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

func _decodeInteger(bencodedNumber string) (int, int, error) {
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

func _decodeList(bencodedList string) ([]interface{}, int, error) {
	emptyList := make([]interface{}, 0)
	if bencodedList == "le" {
		return emptyList, 0, nil
	}
	var elements []interface{}
	lastElementEndIndex := 0
	for bencodedList[lastElementEndIndex+1] != 'e' {
		element, elementLength, err := decodeBencodeData(bencodedList[lastElementEndIndex+1:])
		if err != nil {
			return emptyList, 0, err
		}
		elements = append(elements, element)
		lastElementEndIndex += elementLength
	}
	return elements, lastElementEndIndex + 2, nil
}

func _decodeDict(bencodedDict string) (map[string]interface{}, int, error) {
	emptyDict := make(map[string]interface{})
	if bencodedDict == "de" {
		return emptyDict, 0, nil
	}
	dict := emptyDict
	lastElementEndIndex := 0
	for bencodedDict[lastElementEndIndex+1] != 'e' {
		key, keyLength, err := _decodeString(bencodedDict[lastElementEndIndex+1:])
		if err != nil {
			return emptyDict, 0, err
		}
		value, valueLength, err := decodeBencodeData(bencodedDict[lastElementEndIndex+keyLength+1:])
		if err != nil {
			return emptyDict, 0, err
		}
		dict[key] = value
		lastElementEndIndex += keyLength + valueLength
	}
	return dict, lastElementEndIndex + 2, nil
}

func decodeBencodeData(bencodedString string) (interface{}, int, error) {
	if unicode.IsDigit(rune(bencodedString[0])) {
		return _decodeString(bencodedString)
	}

	if bencodedString[0] == 'i' {
		return _decodeInteger(bencodedString)
	}

	if bencodedString[0] == 'l' {
		return _decodeList(bencodedString)
	}

	if bencodedString[0] == 'd' {
		return _decodeDict(bencodedString)
	}

	return "", 0, fmt.Errorf("unsupported type")
}

func _encodeString(stringToEncode string) string {
	strLength := len(stringToEncode)
	return fmt.Sprintf("%d:%s", strLength, stringToEncode)
}

func _encodeInteger(numberToEncode int) string {
	return fmt.Sprintf("i%de", numberToEncode)
}

func _encodeList(listToEncode []interface{}) string {
	listString := "l"
	for _, value := range listToEncode {
		encodedValue, err := encodeData(value)
		if err != nil {
			panic(err)
		}
		listString += encodedValue
	}
	return listString + "e"
}

func _encodeDict(dictToEncode map[string]interface{}) string {
	dictString := "d"
	keys := make([]string, 0, len(dictToEncode))
	for key := range dictToEncode {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	for _, key := range keys {
		value, ok := dictToEncode[key]
		if !ok {
			panic("Invalid dict")
		}
		dictString += _encodeString(key)
		encodedValue, err := encodeData(value)
		if err != nil {
			panic(err)
		}
		dictString += encodedValue
	}
	return dictString + "e"
}

func encodeData(itemToEncode interface{}) (string, error) {
	switch typedItem := itemToEncode.(type) {
	case string:
		return _encodeString(typedItem), nil
	case int:
		return _encodeInteger(typedItem), nil
	case []interface{}:
		return _encodeList(typedItem), nil
	case map[string]interface{}:
		return _encodeDict(typedItem), nil
	default:
		return "", fmt.Errorf("unsupported type")
	}
}

func main() {
	command := os.Args[1]

	if command == "decode" {
		bencodedValue := os.Args[2]

		decoded, _, err := decodeBencodeData(bencodedValue)
		if err != nil {
			fmt.Println(err)
			return
		}

		jsonOutput, _ := json.Marshal(decoded)
		fmt.Println(string(jsonOutput))
	} else if command == "info" {
		fileName := os.Args[2]

		fileBytes, err := os.ReadFile(fileName)
		if err != nil {
			panic(err)
		}

		decoded, _, err := _decodeDict(string(fileBytes))
		if err != nil {
			panic(err)
		}

		trackerUrl, ok := decoded["announce"].(string)
		if !ok {
			panic("Invalid torrent file")
		}
		fmt.Printf("Tracker URL: %s\n", trackerUrl)
		info, ok := decoded["info"].(map[string]interface{})
		if !ok {
			panic("Invalid torrent file")
		}
		length, ok := info["length"].(int)
		if !ok {
			panic("Invalid torrent file")
		}
		fmt.Printf("Length: %d\n", length)

		encodedInfo, err := encodeData(info)
		if err != nil {
			panic(err)
		}
		fmt.Printf("Info Hash: %x\n", sha1.Sum([]byte(encodedInfo)))

		pieceLength, ok := info["piece length"].(int)
		if !ok {
			panic("Invalid torrent file")
		}
		fmt.Printf("Piece Length: %d\n", pieceLength)

		pieces, ok := info["pieces"].(string)
		if !ok {
			panic("Invalid torrent file")
		}
		fmt.Println("Piece Hashes:")
		piecesLength := len(pieces)
		for i := 0; i < piecesLength; i += 20 {
			piece := pieces[i : i+20]
			fmt.Printf("%x\n", piece)
		}
	} else if command == "peers" {
		fileName := os.Args[2]

		fileBytes, err := os.ReadFile(fileName)
		if err != nil {
			panic(err)
		}

		decoded, _, err := _decodeDict(string(fileBytes))
		if err != nil {
			panic(err)
		}

		info, ok := decoded["info"].(map[string]interface{})
		if !ok {
			panic("Invalid torrent file")
		}

		fileLength, ok := info["length"].(int)
		if !ok {
			panic("Invalid torrent file")
		}

		encodedInfo := _encodeDict(info)

		query, err := url.ParseQuery("")
		if err != nil {
			panic(err)
		}

		query.Add("info_hash", fmt.Sprintf("%s", sha1.Sum([]byte(encodedInfo))))
		query.Add("peer_id", "00112233445566778899")
		query.Add("port", "6881")
		query.Add("uploaded", "0")
		query.Add("downloaded", "0")
		query.Add("left", fmt.Sprint(fileLength))
		query.Add("compact", "1")

		trackerUrl, ok := decoded["announce"].(string)
		if !ok {
			panic("Invalid torrent file")
		}

		torrentUrl, err := url.Parse(fmt.Sprintf("%s?%s", trackerUrl, query.Encode()))
		if err != nil {
			panic(err)
		}

		response, err := http.Get(torrentUrl.String())
		if err != nil {
			panic(err)
		}

		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			panic(err)
		}

		decodedBody, _, err := _decodeDict(string(body))
		if err != nil {
			panic(err)
		}

		fmt.Println(body)
		peers, ok := decodedBody["peers"].(string)
		if !ok {
			panic("Invalid peers")
		}

		peersLength := len(peers)
		for i := 0; i < peersLength; i += 6 {
			firstNum := binary.BigEndian.Uint16([]byte(peers[i : i+1]))
			secondNum := binary.BigEndian.Uint16([]byte(peers[i+1 : i+2]))
			thirdNum := binary.BigEndian.Uint16([]byte(peers[i+2 : i+3]))
			fourthNum := binary.BigEndian.Uint16([]byte(peers[i+3 : i+4]))
			port := binary.BigEndian.Uint16([]byte(peers[i+4 : i+6]))

			fmt.Printf(
				"%d.%d.%d.%d:%d\n",
				firstNum,
				secondNum,
				thirdNum,
				fourthNum,
				port,
			)
		}

	} else {
		fmt.Println("Unknown command: " + command)
		os.Exit(1)
	}
}
