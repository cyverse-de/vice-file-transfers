#!/bin/bash

DATA_STORE_PATH=$1
ZONE_PATH=$2

if [ ! -L ./data ]; then
    ln -s $DATA_STORE_PATH data
fi

if [ ! -L ./home ]; then
    ln -s $ZONE_PATH/home .
fi
