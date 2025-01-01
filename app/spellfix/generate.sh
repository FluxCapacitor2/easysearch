#!/bin/sh -eu

revision=bcc42ef3fd29429bc01a83e751332b8d4690e65d45008449bdffe7656371487f

if [ ! -f spellfix.c ]; then
    wget -nc -O spellfix.c "https://www.sqlite.org/src/raw/$revision?at=spellfix.c"
fi
