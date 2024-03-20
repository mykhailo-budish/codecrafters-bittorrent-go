package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"unicode"
)

var PIECE_BLOCK_MAX_SIZE = 1 << 14

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

	return number, numberEnd + 1, nil
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

func getDecodedFile(torrentFileName string) map[string]interface{} {
	fileBytes, err := os.ReadFile(torrentFileName)
	if err != nil {
		panic(err)
	}

	decodedTorrentFile, _, err := _decodeDict(string(fileBytes))
	if err != nil {
		panic(err)
	}
	return decodedTorrentFile
}

func downloadPiece(decodedTorrentFile map[string]interface{}, pieceIndex int) (piece []byte, pieceLength int) {
	torrentFileInfo, ok := decodedTorrentFile["info"].(map[string]interface{})
	if !ok {
		panic("Invalid torrent file")
	}

	fileLength, ok := torrentFileInfo["length"].(int)
	if !ok {
		panic("Invalid torrent file")
	}

	encodedInfo := _encodeDict(torrentFileInfo)

	trackerUrl, ok := decodedTorrentFile["announce"].(string)
	if !ok {
		panic("Invalid torrent file")
	}

	client := &http.Client{}
	req, err := http.NewRequest(http.MethodGet, trackerUrl, nil)
	if err != nil {
		fmt.Println(err)
		return nil, 0
	}

	query := req.URL.Query()
	query.Add("info_hash", fmt.Sprintf("%s", sha1.Sum([]byte(encodedInfo))))
	query.Add("peer_id", "05022003050220034586")
	query.Add("port", "6881")
	query.Add("uploaded", "0")
	query.Add("downloaded", "0")
	query.Add("left", fmt.Sprint(fileLength))
	query.Add("compact", "1")

	req.URL.RawQuery = query.Encode()

	response, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
		return nil, 0
	}

	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		fmt.Println(err)
		return nil, 0
	}

	decodedBody, _, err := _decodeDict(string(responseBody))
	if err != nil {
		fmt.Println(string(responseBody))
		panic(err)
	}

	peers, ok := decodedBody["peers"].(string)
	if !ok {
		fmt.Println(string(responseBody))
	}

	address := fmt.Sprintf("%d.%d.%d.%d:%d", peers[0], peers[1], peers[2], peers[3], int(peers[4])*256+int(peers[5]))

	fmt.Printf("%s\n", address)
	conn, err := net.Dial("tcp", address)
	if err != nil {
		panic(err)
	}
	defer conn.Close()
	pstrlen := byte(19)
	pstr := []byte("BitTorrent protocol")
	reserved := make([]byte, 8)
	handshake := append([]byte{pstrlen}, pstr...)
	handshake = append(handshake, reserved...)
	handshake = append(handshake, []byte(fmt.Sprintf("%s", sha1.Sum([]byte(encodedInfo))))...)
	handshake = append(handshake, []byte("00112233445566778899")...)

	_, err = conn.Write(handshake)
	if err != nil {
		panic(err)
	}

	buf := make([]byte, len(handshake))
	bytesRead, err := conn.Read(buf)
	if err != nil {
		fmt.Println(err)
		return nil, 0
	}
	message := string(buf[:bytesRead])
	fmt.Printf("Peer ID: %x\n", message[len(message)-20:])

	buf = make([]byte, 1<<15)
	_, err = conn.Read(buf)
	if err != nil {
		panic(err)
	}
	messageCode := buf[4]
	if messageCode != 5 {
		panic("Received unexpected message, expected bitfield")
	}
	fmt.Println("Got bitfield")
	payloadBuffer := new(bytes.Buffer)
	binary.Write(payloadBuffer, binary.BigEndian, uint32(1))
	payloadBytes := payloadBuffer.Bytes()
	payloadBytes = append(payloadBytes, 2)
	conn.Write(payloadBytes)
	_, err = conn.Read(buf)
	if err != nil {
		panic(err)
	}
	messageCode = buf[4]
	if messageCode != 1 {
		panic("Received unexpected message, expected unchoke")
	}
	fmt.Println("got unchoke")
	piecesAmount := len(torrentFileInfo["pieces"].(string)) / 20
	pieceLength = torrentFileInfo["piece length"].(int)
	if pieceIndex == piecesAmount-1 {
		pieceLength = fileLength - pieceLength*(piecesAmount-1)
	}
	pieceBlocksAmount := pieceLength / PIECE_BLOCK_MAX_SIZE
	if pieceLength%PIECE_BLOCK_MAX_SIZE > 0 {
		pieceBlocksAmount++
	}
	piece = make([]byte, pieceLength)
	for i := 0; i < pieceLength; i += PIECE_BLOCK_MAX_SIZE {
		pieceBlockLength := PIECE_BLOCK_MAX_SIZE
		if i+PIECE_BLOCK_MAX_SIZE > pieceLength {
			pieceBlockLength = pieceLength - i
		}

		piecePayloadBuffer := new(bytes.Buffer)
		binary.Write(piecePayloadBuffer, binary.BigEndian, uint32(13))
		piecePayload := piecePayloadBuffer.Bytes()

		piecePayload = append(piecePayload, 6)

		piecePayloadBuffer = new(bytes.Buffer)
		binary.Write(piecePayloadBuffer, binary.BigEndian, uint32(pieceIndex))
		piecePayload = append(piecePayload, piecePayloadBuffer.Bytes()...)

		piecePayloadBuffer = new(bytes.Buffer)
		binary.Write(piecePayloadBuffer, binary.BigEndian, uint32(i))
		piecePayload = append(piecePayload, piecePayloadBuffer.Bytes()...)

		piecePayloadBuffer = new(bytes.Buffer)
		binary.Write(piecePayloadBuffer, binary.BigEndian, uint32(pieceBlockLength))
		piecePayload = append(piecePayload, piecePayloadBuffer.Bytes()...)

		fmt.Printf("%v\n", piecePayload)
		_, err = conn.Write(piecePayload)
		if err != nil {
			fmt.Printf("Error sending message: %s", err.Error())
			return nil, 0
		}
		fmt.Printf("Begin: %d, length: %d, piece length: %d, blocks amount: %d\n", i, pieceBlockLength, pieceLength, pieceBlocksAmount)

		var messageLength uint32
		var messageId uint8
		err = binary.Read(conn, binary.BigEndian, &messageLength)
		if err != nil {
			if err.Error() == "EOF" {
				// i -= PIECE_BLOCK_MAX_SIZE
				break
			}
		}
		err = binary.Read(conn, binary.BigEndian, &messageId)
		if err != nil {
			fmt.Printf("Cannot read message ID, %s", err.Error())
			return nil, 0
		}
		fmt.Println(messageId)

		if messageLength > 0 {
			buf = make([]byte, messageLength-1)
			_, err = io.ReadAtLeast(conn, buf, len(buf))
			if err != nil {
				fmt.Printf("Cannot read payload, %s", err.Error())
				return nil, 0
			}
			blockBegin := binary.BigEndian.Uint32(buf[4:8])
			block := buf[8:]
			copy(piece[blockBegin:], block)
		}
	}
	pieceSumFromPeer := sha1.Sum(piece)
	pieceHashFromFile := []byte(torrentFileInfo["pieces"].(string)[pieceIndex*20 : (pieceIndex+1)*20])
	if !bytes.Equal(pieceSumFromPeer[:], pieceHashFromFile) {
		panic("Invalid piece checksum")
	}
	return piece, pieceLength
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

		trackerUrl, ok := decoded["announce"].(string)
		if !ok {
			panic("Invalid torrent file")
		}

		client := &http.Client{}
		req, err := http.NewRequest(http.MethodGet, trackerUrl, nil)
		if err != nil {
			fmt.Println(err)
			return
		}

		query := req.URL.Query()
		query.Add("info_hash", fmt.Sprintf("%s", sha1.Sum([]byte(encodedInfo))))
		query.Add("peer_id", "05022003050220034586")
		query.Add("port", "6881")
		query.Add("uploaded", "0")
		query.Add("downloaded", "0")
		query.Add("left", fmt.Sprint(fileLength))
		query.Add("compact", "1")

		req.URL.RawQuery = query.Encode()

		response, err := client.Do(req)
		if err != nil {
			fmt.Println(err)
			return
		}

		defer response.Body.Close()
		responseBody, err := io.ReadAll(response.Body)
		if err != nil {
			fmt.Println(err)
			return
		}

		decodedBody, _, err := _decodeDict(string(responseBody))
		if err != nil {
			fmt.Println(string(responseBody))
			panic(err)
		}

		peers, ok := decodedBody["peers"].(string)
		if !ok {
			fmt.Println(string(responseBody))
		}

		peersLength := len(peers)
		for i := 0; i < peersLength; i += 6 {
			ip := peers[i : i+4]
			port := peers[i+4 : i+6]

			fmt.Printf(
				"%d.%d.%d.%d:%d\n",
				ip[0],
				ip[1],
				ip[2],
				ip[3],
				int(port[0])*256+int(port[1]),
			)
		}

	} else if command == "handshake" {
		fileName := os.Args[2]
		address := os.Args[3]

		// address := "178.62.85.20:51489"

		fileBytes, err := os.ReadFile(fileName)
		if err != nil {
			panic(err)
		}

		decoded, _, err := _decodeDict(string(fileBytes))
		if err != nil {
			panic(err)
		}
		info := decoded["info"].(map[string]interface{})
		encodedInfo := _encodeDict(info)

		conn, err := net.Dial("tcp", address)
		if err != nil {
			panic(err)
		}
		pstrlen := byte(19)
		pstr := []byte("BitTorrent protocol")
		reserved := make([]byte, 8)
		handshake := append([]byte{pstrlen}, pstr...)
		handshake = append(handshake, reserved...)
		handshake = append(handshake, []byte(fmt.Sprintf("%s", sha1.Sum([]byte(encodedInfo))))...)
		handshake = append(handshake, []byte("00112233445566778899")...)

		_, err = conn.Write(handshake)
		if err != nil {
			panic(err)
		}

		buf := make([]byte, len(handshake))
		bytesRead, err := conn.Read(buf)
		if err != nil {
			fmt.Println(err)
			return
		}
		message := string(buf[:bytesRead])
		fmt.Printf("Peer ID: %x\n", message[len(message)-20:])

	} else if command == "download_piece" {
		if os.Args[2] != "-o" {
			panic("Output file is not provided")
		}
		outputFilePath := os.Args[3]
		torrentFileName := os.Args[4]
		pieceIndex, err := strconv.Atoi(os.Args[5])
		if err != nil {
			panic(err)
		}

		piece, _ := downloadPiece(getDecodedFile(torrentFileName), pieceIndex)
		os.WriteFile(outputFilePath, piece, os.ModePerm)
		fmt.Printf("Piece %d downloaded to %s.", pieceIndex, outputFilePath)
	} else if command == "download" {
		outputFilePath := os.Args[3]
		torrentFileName := os.Args[4]
		decodedTorrentFile := getDecodedFile(torrentFileName)

		torrentFileInfo := decodedTorrentFile["info"].(map[string]interface{})
		fileLength := torrentFileInfo["length"].(int)

		piecesAmount := len(torrentFileInfo["pieces"].(string)) / 20
		wholePieceLength := torrentFileInfo["piece length"].(int)

		fileBytes := make([]byte, fileLength)
		for i := 0; i < piecesAmount; i++ {
			piece, pieceLength := downloadPiece(decodedTorrentFile, i)
			copy(fileBytes[i*wholePieceLength:i*wholePieceLength+pieceLength], piece)
		}
		os.WriteFile(outputFilePath, fileBytes, os.ModePerm)
		fmt.Printf("Downloaded %s to %s.", torrentFileName, outputFilePath)
	} else {
		fmt.Println("Unknown command: " + command)
		os.Exit(1)
	}
}
