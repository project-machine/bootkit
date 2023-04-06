#!/bin/bash
. ${LIB_DRACUT_D:-/usr/lib/dracut}/soci-lib.sh

mount_dev() {
    local name="$1" mp="$2"
    mount "$name" "$mp"
}

short2dev() {
    # turn 'LABEL=' or 'UUID=' into a device path
    # also support /dev/* and 'vdb' or 'xvda'
    local input="$1" dev
    case "$input" in
        LABEL=*)
            dev="${input#LABEL=}"
            case "${dev}" in
                */*) dev="$(echo "${dev}" | sed 's,/,\\x2f,g')";;
            esac
            dev="/dev/disk/by-label/${dev}"
            ;;
        UUID=*) dev="/dev/disk/by-uuid/${input#UUID=}" ;;
        ID=*)
            # TODO: fix ID= to support any id /dev/disk/by-id/*-<id>
            # as the disk id is only exposed as /dev/disk/by-id/<path>-<id>
            # where 'path' is like 'virtio'.  But why would someone care
            # how the device with a given serial was attached.
            dev="/dev/disk/by-id/${input##ID=}";;
        /dev/*) dev="${input}";;
        *) dev="/dev/${input}";;
    esac
    _RET=$dev
}

# try_modules(imgpath, rootd)
# opportunistically mount an image at <imgpath> to <rootd>/lib/modules/$(uname -r)
try_modules() {
    local modsquash="$1" rootd="$2"
    [ -f "$modsquash" ] || {
        soci_debug "no modules.squashfs at $1"
        return 0
    }
    local kver="" mdir=""
    kver=$(uname -r)
    mdir="$rootd/lib/modules/$kver"
    if [ -f "$mdir/modules.dep" ]; then
        soci_debug "modules for $kver already existed in lib/modules/$kver under root '$rootd'"
        return 0
    fi

    [ -d "$rootd/lib/modules" ] || mkdir -p "$rootd/lib/modules" || {
        soci_warn "Could not create lib/modules under '$rootd'"
        return 0
    }

    if ! soci_log_run mount "$modsquash" "$rootd/lib/modules"; then
        soci_warn "failed to mount $modsquash"
        return 1
    fi

    soci_info "mounted modules to /lib/modules"
    [ -d "$rootd/lib/modules/$kver" ] || {
        soci_warn "no modules for version '$kver' in $modsquash"
        return 1
    }
}


wait_on_zot() {
	count=5
	up=0
	while [[ $count -gt 0 ]]; do
		count=$((count - 1))
        soci_debug "Waiting on zot: $count/5"
		if [ ! -d /proc/$pid ]; then
			soci_die "zot failed to start or died"
			exit 1
		fi
		up=1
		soci_log_run curl -f http://127.0.0.1:5000/v2/ || up=0
		if [ $up -eq 1 ]; then break; fi
		sleep 1
	done
	if [ $up -eq 0 ]; then
		soci_die "Timed out waiting for zot"
		exit 1
	fi
}

start_zot() {
    cat > /etc/systemd/system/soci-zot.service << EOF
[Unit]
Description=Start zot for soci mount

[Service]
Type=simple
ExecStart=/usr/bin/zot serve /etc/zot-config.json
StandardInput=tty-force
StandardOutput=inherit
StandardError=inherit

[Install]
WantedBy=multi-user.target
EOF

    systemctl enable soci-zot.service
    systemctl start soci-zot.service
    wait_on_zot
}

soci_udev_settled() {
    ${SOCI_ENABLED} || return 0
    # if SOCI_dev is set, wait for it.
    local dev="${SOCI_dev}" path="${SOCI_path}" name="${SOCI_name}" devpath="" repo="${SOCI_repo}"

    short2dev "$dev"
    devpath="$_RET"

    if [ ! -b "$devpath" ]; then
        soci_debug "$devpath did not exist yet"
        return 0
    fi

    local dmp="/run/initramfs/.socidev"
    if ! ismounted "$dmp"; then
        mkdir -p "$dmp" || {
            soci_die "Failed to create dir '$dmp'"
            return 1
        }
        soci_debug "mounting $devpath to $dmp"
        mount -o ro "$devpath" "$dmp" || {
            soci_die "Failed to mount $devpath -> $dmp"
            return 1
        }
        [ -e "$dmp/$path" ] || {
            soci_die "oci repo path '$path' did not exist on device '$dev'"
            return 1
        }

        [ -d "$dmp/$path" ] || {
            soci_die "oci repo path '$path' was not a directory on '$dev'"
            return 1
        }
    fi

    if [ ! -e "${SOCI_FINISHED_MARK}" ]; then
        local debug="" rootd="$NEWROOT" rfs="/run/rfs"
        local lower="$rfs/lower" upper="$rfs/upper" work="$rfs/work"
        mkdir -p "$lower" "$upper" "$work" || {
            soci_die "could not create directories: '$lower', '$upper', '$work'"
        }

        if [ -n "$repo" -a "$repo" = "local" ]; then
            # our zot config expects to find its cache under /oci
            mkdir -p /oci
            mount --bind "${dmp}/oci" /oci
            soci_debug "Starting a zot service"
            start_zot
            export repo="127.0.0.1:5000"
        fi

        [ "$SOCI_DEBUG" = "true" ] && debug="--debug"
        mkdir -p /factory/secure
        chmod 700 /factory/secure
        cp /manifestCA.pem /factory/secure/

        mkdir -p /config /scratch-writes /atomfs-store
        find /oci
        set -- mosctl $debug mount \
            "--target=livecd" \
            "--dest=$rootd" \
            "${repo}/$name"

        if soci_log_run "$@"; then
            soci_info "successfully ran: $*"
        else
            ret=$?
            out=$(curl http://127.0.0.1:5000/v2/_catalog)
            soci_info "catalog: ${out}"
            out=$(curl http://127.0.0.1:5000/v2/machine/livecd/tags/list)
            soci_info "tags: ${out}"
            out=$(curl http://127.0.0.1:5000/v2/machine/livecd/manifests/1.0.0)
            soci_info "manifest: ${out}"
            soci_info "log: $(</run/initramfs/log)"
            soci_die "extract-soci '$name' '$rootd' failed with exit code $ret"
            return 1
        fi

        soci_log_run cat /proc/self/mounts
        soci_log_run cat /proc/modules
        soci_log_run stat /sysroot
        soci_log_run ls -l $lower

        try_modules "$dmp/krd/modules.squashfs" "$rootd" || {
            soci_die "Failed mounting modules"
            return 1
        }

        : > "${SOCI_FINISHED_MARK}"
        # if layer was 'tar' and there were no modules, then we can could unmount the iso fs.
        # if modules are mounted, or squashfs type layer, then this will fail.
        out=$(umount "$dmp" 2>&1) ||
            soci_debug "umount $dmp did did not succeed. Probably squashfs."
    fi
    return 0
}

soci_set_vars
soci_udev_settled || soci_die "soci_udev_settled failed"
