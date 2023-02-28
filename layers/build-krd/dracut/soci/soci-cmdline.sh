#!/bin/bash
# parse the command line, set rootok.

. ${LIB_DRACUT_D:-/usr/lib/dracut}/soci-lib.sh

soci_initrd_start() {
    soci_var_info
    $SOCI_ENABLED || return
    # this sets the global dracut variables 'rootok' and 'root'
    rootok=1
    root=${SOCI_ROOT}
}

soci_set_vars
soci_initrd_start
