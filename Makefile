###################################################################################################################

MAKEFLAGS += --silent
SHELL := /bin/bash
BIN_NAME := tft
.DEFAULT_GOAL := build

###################################################################################################################

build:
	echo "Compiling $(BIN_NAME)..."
	go build -o $(BIN_NAME)

.PHONY: build

clean:
	rm -f $(BIN_NAME)