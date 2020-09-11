#!/usr/bin/env bash
go build -o RXclientUpdater.bin
GOOS=windows GOARCH=386 go build -o RXclientUpdater.exe
