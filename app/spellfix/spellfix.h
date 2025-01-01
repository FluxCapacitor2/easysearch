#ifndef SQLITE_SPELLFIX_H
#define SQLITE_SPELLFIX_H

#ifndef SQLITE_CORE
#include "sqlite3ext.h"
#else
#include "sqlite3.h"
#endif

int sqlite3_spellfix_init(
    sqlite3 *db, 
    char **pzErrMsg, 
    const sqlite3_api_routines *pApi
);

#endif /* ifndef SQLITE_SPELLFIX_H */
