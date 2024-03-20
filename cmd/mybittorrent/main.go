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

const (
	ChokeMessage         uint8 = 0
	UnchokeMessage       uint8 = 1
	InterestedMessage    uint8 = 2
	NotInterestedMessage uint8 = 3
	HaveMessage          uint8 = 4
	BitFieldMessage      uint8 = 5
	RequestMessage       uint8 = 6
	PieceMessage         uint8 = 7
	CancelMessage        uint8 = 8
)

type PeerMessage struct {
	length  uint32
	tag     uint8
	payload []byte
}

func (peerMessage PeerMessage) toBytes() []byte {
	payloadLength := len(peerMessage.payload)
	messageBytes := make([]byte, 5+payloadLength)
	binary.BigEndian.PutUint32(messageBytes[:4], peerMessage.length)
	messageBytes[4] = peerMessage.tag
	copy(messageBytes[5:], peerMessage.payload)
	return messageBytes
}
func readPeerMessageFromConnection(conn net.Conn) PeerMessage {
	var length uint32
	var tag uint8

	err := binary.Read(conn, binary.BigEndian, &length)
	if err != nil {
		panic("Unable to parse message length")
	}

	err = binary.Read(conn, binary.BigEndian, &tag)
	if err != nil {
		panic("Unable to parse message type")
	}

	payload := make([]byte, length-1)
	io.ReadAtLeast(conn, payload, len(payload))
	peerMessage := PeerMessage{
		length:  length,
		tag:     tag,
		payload: payload,
	}
	return peerMessage
}

type RequestMessagePayload struct {
	index  uint32
	begin  uint32
	length uint32
}

func (requestMessagePayload RequestMessagePayload) toBytes() []byte {
	payloadBytes := make([]byte, 12)
	binary.BigEndian.PutUint32(payloadBytes[0:4], requestMessagePayload.index)
	binary.BigEndian.PutUint32(payloadBytes[4:8], requestMessagePayload.begin)
	binary.BigEndian.PutUint32(payloadBytes[8:12], requestMessagePayload.length)
	return payloadBytes
}

type PieceMessagePayload struct {
	index uint32
	begin uint32
	block []byte
}

func getPieceMessagePayload(messagePayload []byte) PieceMessagePayload {
	pieceMessagePayload := PieceMessagePayload{
		index: binary.BigEndian.Uint32(messagePayload[:4]),
		begin: binary.BigEndian.Uint32(messagePayload[4:8]),
		block: messagePayload[8:],
	}
	return pieceMessagePayload
}

type TorrentFileInfo struct {
	length      int
	pieceLength int
	pieces      [][]byte
}
type TorrentFile struct {
	trackerUrl string
	info       TorrentFileInfo
	rawInfo    map[string]interface{}
}

func (torrentFile TorrentFile) getInfoHash() [20]byte {
	encodedInfo, err := encodeData(torrentFile.rawInfo)
	if err != nil {
		panic(err)
	}
	return sha1.Sum([]byte(encodedInfo))
}
func (torrentFile TorrentFile) getPeers() []string {
	client := &http.Client{}
	req, err := http.NewRequest(http.MethodGet, torrentFile.trackerUrl, nil)
	if err != nil {
		panic(err)
	}

	query := req.URL.Query()
	query.Add("info_hash", fmt.Sprintf("%s", torrentFile.getInfoHash()))
	query.Add("peer_id", "05022003050220034586")
	query.Add("port", "6881")
	query.Add("uploaded", "0")
	query.Add("downloaded", "0")
	query.Add("left", fmt.Sprint(torrentFile.info.length))
	query.Add("compact", "1")

	req.URL.RawQuery = query.Encode()

	response, err := client.Do(req)
	if err != nil {
		panic(err)
	}

	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		panic(err)
	}

	decodedBody, _, err := _decodeDict(string(responseBody))
	if err != nil {
		fmt.Println(string(responseBody))
		panic(err)
	}

	peersBytes, ok := decodedBody["peers"].(string)
	if !ok {
		fmt.Println(string(responseBody))
		panic("Cannot parse peers")
	}

	peersAmount := len(peersBytes) / 6
	peers := make([]string, peersAmount)
	for i := 0; i < peersAmount; i++ {
		peerBytes := peersBytes[i*6 : (i+1)*6]
		peers[i] = fmt.Sprintf(
			"%d.%d.%d.%d:%d",
			peerBytes[0],
			peerBytes[1],
			peerBytes[2],
			peerBytes[3],
			binary.BigEndian.Uint16([]byte(peerBytes[4:6])),
		)
	}
	return peers
}
func (torrentFile TorrentFile) setupHandshake(address string) net.Conn {
	conn, err := net.Dial("tcp", address)
	if err != nil {
		panic(err)
	}
	pstrlen := byte(19)
	pstr := []byte("BitTorrent protocol")
	reserved := make([]byte, 8)
	handshake := append([]byte{pstrlen}, pstr...)
	handshake = append(handshake, reserved...)
	handshake = append(handshake, []byte(fmt.Sprintf("%s", torrentFile.getInfoHash()))...)
	handshake = append(handshake, []byte("00112233445566778899")...)

	_, err = conn.Write(handshake)
	if err != nil {
		panic(err)
	}

	buf := make([]byte, len(handshake))
	bytesRead, err := conn.Read(buf)
	if err != nil {
		panic(err)
	}
	message := string(buf[:bytesRead])
	fmt.Printf("Peer ID: %x\n", message[len(message)-20:])
	return conn
}

func (torrentFile TorrentFile) downloadPiece(pieceIndex int) (piece []byte, pieceLength int) {
	peers := torrentFile.getPeers()
	conn := torrentFile.setupHandshake(peers[0])

	bitFieldMessage := readPeerMessageFromConnection(conn)
	if bitFieldMessage.tag != BitFieldMessage {
		panic("Received unexpected message, expected bitfield")
	}
	fmt.Println("Received bitfield")

	interestedMessage := PeerMessage{
		length:  1,
		tag:     InterestedMessage,
		payload: make([]byte, 0),
	}
	conn.Write(interestedMessage.toBytes())

	unchokeMessage := readPeerMessageFromConnection(conn)
	if unchokeMessage.tag != UnchokeMessage {
		panic("Received unexpected message, expected unchoke")
	}
	fmt.Println("received unchoke")

	piecesAmount := len(torrentFile.info.pieces)
	pieceLength = torrentFile.info.pieceLength
	if pieceIndex == piecesAmount-1 {
		pieceLength = torrentFile.info.length - pieceLength*(pieceLength-1)
	}

	piece = make([]byte, pieceLength)
	for pieceBlockBegin := 0; pieceBlockBegin < pieceLength; pieceBlockBegin += PIECE_BLOCK_MAX_SIZE {
		pieceBlockLength := PIECE_BLOCK_MAX_SIZE
		if pieceBlockBegin+PIECE_BLOCK_MAX_SIZE > pieceLength {
			pieceBlockLength = pieceLength - pieceBlockBegin
		}
		requestMessagePayload := RequestMessagePayload{
			index:  uint32(pieceIndex),
			begin:  uint32(pieceBlockBegin),
			length: uint32(pieceBlockLength),
		}
		requestMessagePayloadBytes := requestMessagePayload.toBytes()
		requestMessage := PeerMessage{
			length:  uint32(1 + len(requestMessagePayloadBytes)),
			tag:     RequestMessage,
			payload: requestMessagePayloadBytes,
		}
		conn.Write(requestMessage.toBytes())
		fmt.Printf("Begin: %d, length: %d, piece length: %d\n", pieceBlockBegin, pieceBlockLength, pieceLength)

		pieceMessage := readPeerMessageFromConnection(conn)
		if pieceMessage.tag != PieceMessage {
			panic("Received unexpected message, expected piece")
		}
		pieceMessagePayload := getPieceMessagePayload(pieceMessage.payload)
		copy(piece[pieceBlockBegin:pieceBlockBegin+pieceBlockLength], pieceMessagePayload.block)
	}
	pieceSumFromPeer := sha1.Sum(piece)
	pieceHashFromFile := torrentFile.info.pieces[pieceIndex]
	if !bytes.Equal(pieceSumFromPeer[:], pieceHashFromFile) {
		panic("Invalid piece checksum")
	}
	return piece, pieceLength
}

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

func getDecodedTorrentFile(torrentFileName string) TorrentFile {
	fileBytes, err := os.ReadFile(torrentFileName)
	if err != nil {
		panic(err)
	}

	decodedTorrentFile, _, err := _decodeDict(string(fileBytes))
	if err != nil {
		panic(err)
	}
	torrentFileInfo := decodedTorrentFile["info"].(map[string]interface{})
	piecesString := torrentFileInfo["pieces"].(string)
	pieces := make([][]byte, len(piecesString)/20)
	for i := 0; i < len(pieces); i++ {
		pieces[i] = []byte(piecesString[i*20 : (i+1)*20])
	}
	torrentFile := TorrentFile{
		trackerUrl: decodedTorrentFile["announce"].(string),
		info: TorrentFileInfo{
			length:      torrentFileInfo["length"].(int),
			pieceLength: torrentFileInfo["piece length"].(int),
			pieces:      pieces,
		},
		rawInfo: torrentFileInfo,
	}
	return torrentFile
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
		torrentFile := getDecodedTorrentFile(fileName)

		fmt.Printf("Tracker URL: %s\n", torrentFile.trackerUrl)
		fmt.Printf("Length: %d\n", torrentFile.info.length)
		fmt.Printf("Info Hash: %x\n", torrentFile.getInfoHash())
		fmt.Printf("Piece Length: %d\n", torrentFile.info.pieceLength)

		fmt.Println("Piece Hashes:")
		for _, piece := range torrentFile.info.pieces {
			fmt.Printf("%x\n", piece)
		}
	} else if command == "peers" {
		fileName := os.Args[2]

		torrentFile := getDecodedTorrentFile(fileName)

		for _, peer := range torrentFile.getPeers() {
			fmt.Println(peer)
		}
	} else if command == "handshake" {
		fileName := os.Args[2]
		address := os.Args[3]

		torrentFile := getDecodedTorrentFile(fileName)
		torrentFile.setupHandshake(address)
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

		torrentFile := getDecodedTorrentFile(torrentFileName)
		piece, _ := torrentFile.downloadPiece(pieceIndex)
		os.WriteFile(outputFilePath, piece, os.ModePerm)
		fmt.Printf("Piece %d downloaded to %s.", pieceIndex, outputFilePath)
	} else if command == "download" {
		outputFilePath := os.Args[3]
		torrentFileName := os.Args[4]
		torrentFile := getDecodedTorrentFile(torrentFileName)
		wholePieceLength := torrentFile.info.pieceLength

		fileBytes := make([]byte, torrentFile.info.length)
		for i := range torrentFile.info.pieces {
			piece, pieceLength := torrentFile.downloadPiece(i)
			copy(fileBytes[i*wholePieceLength:i*wholePieceLength+pieceLength], piece)
		}
		os.WriteFile(outputFilePath, fileBytes, os.ModePerm)
		fmt.Printf("Downloaded %s to %s.", torrentFileName, outputFilePath)
	} else {
		fmt.Println("Unknown command: " + command)
		os.Exit(1)
	}
}
