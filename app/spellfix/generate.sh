#!/bin/sh -eu

revision=bcc42ef3fd29429bc01a83e751332b8d4690e65d45008449bdffe7656371487f

if [ ! -f src/spellfix.c ]; then
    wget -nc -O src/spellfix.c "https://www.sqlite.org/src/raw/$revision?at=spellfix.c"
fi
