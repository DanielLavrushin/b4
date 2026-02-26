#!/bin/sh
# This is the core installation part script for b4 Universal.
# Install b4 binary
install_b4() {
    arch="$1"
    version="$2"

    # Construct download URL
    file_name="${BINARY_NAME}-linux-${arch}.tar.gz"
    download_url="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/download/${version}/${file_name}"
    archive_path="${TEMP_DIR}/${file_name}"

    # Download the archive with checksum verification
    if ! download_file "$download_url" "$archive_path" "$version" "$arch"; then
        print_error "Failed to download b4 for architecture: $arch"
        exit 1
    fi

    rm -f "/opt/etc/init.d/S99b4" 2>/dev/null || true # remove legacy script
    rm -f "/etc/init.d/b4" 2>/dev/null || true        # remove legacy script
    rm -f "/var/log/b4.log" 2>/dev/null || true       # remove legacy log

    # Extract the binary
    print_info "Extracting archive..."
    cd "$TEMP_DIR"
    tar -xzf "$archive_path" || {
        print_error "Failed to extract archive"
        exit 1
    }

    # Check if binary exists
    if [ ! -f "${BINARY_NAME}" ]; then
        print_error "Binary not found in archive"
        exit 1
    fi

    # Stop existing b4 if running
    stop_process "$BINARY_NAME"

    # Create timestamp in POSIX way
    timestamp=$(date '+%Y%m%d_%H%M%S')
    BACKUP_FILE="${INSTALL_DIR}/${BINARY_NAME}.backup.${timestamp}"

    # Backup existing binary if it exists
    if [ -f "${INSTALL_DIR}/${BINARY_NAME}" ]; then
        print_info "Backing up existing binary..."
        mv "${INSTALL_DIR}/${BINARY_NAME}" "$BACKUP_FILE"
    fi

    # Install the new binary
    print_info "Installing b4 to ${INSTALL_DIR}..."
    cp "${BINARY_NAME}" "${INSTALL_DIR}/" || {
        print_error "Failed to copy binary to install directory"
        exit 1
    }

    # Set executable permissions
    chmod +x "${INSTALL_DIR}/${BINARY_NAME}" || {
        print_error "Failed to set executable permissions"
        exit 1
    }

    # Verify installation
    if "${INSTALL_DIR}/${BINARY_NAME}" --version >/dev/null 2>&1; then
        [ -n "$BACKUP_FILE" ] && rm -f "$BACKUP_FILE" 2>/dev/null || true
        # Clean old backups
        rm -f "${INSTALL_DIR}/${BINARY_NAME}".backup.* 2>/dev/null || true
        print_success "b4 installed successfully!"
    else
        print_warning "Binary installed but version check failed"
    fi
}

# Print web interface access information
print_web_interface_info() {
    local web_port
    local protocol="http"
    
    if [ -f "$CONFIG_FILE" ] && command_exists jq; then
        web_port=$(jq -r '.web_server.port // 7000' "$CONFIG_FILE" 2>/dev/null)
    fi
    
    # Проверяем, есть ли сертификаты uhttpd, которые мы используем для HTTPS
    if [ -f "/etc/uhttpd.crt" ] && [ -f "/etc/uhttpd.key" ]; then
        protocol="https"
    fi

    echo ""
    echo "======================================="
    echo "  Web Interface Access"
    echo "======================================="
    echo ""

    # Get LAN IP (br0 interface on routers)
    lan_ip=""
    if command_exists ip; then
        lan_ip=$(ip -4 addr show br0 2>/dev/null | grep 'inet ' | awk '{print $2}' | cut -d'/' -f1)
    fi
    if [ -z "$lan_ip" ] && command_exists ifconfig; then
        lan_ip=$(ifconfig br0 2>/dev/null | grep 'inet addr:' | awk '{print $2}' | cut -d':' -f2)
    fi
    if [ -z "$lan_ip" ]; then
        if command_exists ip; then
            lan_ip=$(ip -4 addr show 2>/dev/null | grep 'inet 192.168' | head -n1 | awk '{print $2}' | cut -d'/' -f1)
        elif command_exists ifconfig; then
            lan_ip=$(ifconfig 2>/dev/null | grep 'inet addr:192.168' | head -n1 | awk '{print $2}' | cut -d':' -f2)
        fi
    fi

    if [ -n "$lan_ip" ]; then
        print_info "Local network access (LAN):"
        # Выводим ссылку с учетом протокола (http/https)
        printf "        ${GREEN}%s://%s:%s${NC}\n" "$protocol" "$lan_ip" "$web_port"
        printf "        (remember to start the service first)\n"
    fi

    echo ""
}

    # Get LAN IP (br0 interface on routers)
    lan_ip=""

    # Try to get IP from br0 specifically (most common on routers)
    if command_exists ip; then
        lan_ip=$(ip -4 addr show br0 2>/dev/null | grep 'inet ' | awk '{print $2}' | cut -d'/' -f1)
    fi

    # Fallback to ifconfig
    if [ -z "$lan_ip" ] && command_exists ifconfig; then
        lan_ip=$(ifconfig br0 2>/dev/null | grep 'inet addr:' | awk '{print $2}' | cut -d':' -f2)
    fi

    # If br0 didn't work, look for any 192.168.x.x address
    if [ -z "$lan_ip" ]; then
        if command_exists ip; then
            lan_ip=$(ip -4 addr show 2>/dev/null | grep 'inet 192.168' | head -n1 | awk '{print $2}' | cut -d'/' -f1)
        elif command_exists ifconfig; then
            lan_ip=$(ifconfig 2>/dev/null | grep 'inet addr:192.168' | head -n1 | awk '{print $2}' | cut -d':' -f2)
        fi
    fi

    # Print local/LAN access
    if [ -n "$lan_ip" ]; then
        print_info "Local network access (LAN):"
        printf "        ${GREEN}http://%s:%s${NC}\n" "$lan_ip" "$web_port"
        printf "        (remember to start the service first)\n"
    fi

    # # Get external IP
    # print_info "Checking external IP address..."
    # external_ip=""

    # if command_exists curl; then
    #     external_ip=$(curl -s --max-time 3 ifconfig.me 2>/dev/null || true)
    # elif command_exists wget; then
    #     external_ip=$(wget -qO- --timeout=3 ifconfig.me 2>/dev/null || true)
    # fi

    # # Print external access if different from LAN
    # if [ -n "$external_ip" ] && [ "$external_ip" != "$lan_ip" ]; then
    #     print_info "External access (WAN, if port ${web_port} is forwarded):"
    #     printf "        ${GREEN}http://%s:%s${NC}\n" "$external_ip" "$web_port"
    #     print_warning "Note: Ensure port ${web_port} is open in your firewall"
    # fi

    echo ""
}

# Main installation process
main_install() {

    #  get args
    VERSION=""
    for arg in "$@"; do
        case "$arg" in
        v* | V*)
            VERSION="$arg"
            print_info "Using specified version: $VERSION"
            ;;
        --quiet | -q)
            QUIET_MODE=1
            ;;
        --geosite-src=*)
            GEOSITE_SRC="${arg#*=}"
            ;;
        --geosite-dst=*)
            GEOSITE_DST="${arg#*=}"
            ;;
        esac
    done

    # Detect system and set paths
    set_system_paths

    if [ "$QUIET_MODE" = "0" ]; then
        echo ""
        echo "======================================="
        echo "     B4 Universal Installer"
        echo "======================================="
        echo ""
    fi

    # Check if running as root
    check_root

    # Check dependencies
    check_dependencies

    # Detect architecture
    print_info "Detecting system architecture..."
    ARCH=$(detect_architecture)
    print_info "Raw architecture: $(uname -m)"
    print_success "Architecture detected: $ARCH"

    if [ -z "$VERSION" ]; then
        print_info "Fetching latest release information..."
        VERSION=$(get_latest_version)
        print_success "Latest version: $VERSION"
    fi

    # Setup directories
    setup_directories

    # Install b4
    install_b4 "$ARCH" "$VERSION"

    # Create service files
        # Create service files
    create_systemd_service
    if [ "$SYSTEMCTL_CREATED" != "1" ]; then
        create_sysv_service
        
        if [ -f "/etc/init.d/b4" ] && [ -f "/etc/uhttpd.crt" ] && [ -f "/etc/uhttpd.key" ]; then
            print_info "OpenWrt SSL certificates detected. Enabling HTTPS for b4..."
 
            sed -i 's|procd_set_param command $PROG|procd_set_param command $PROG --tls-cert /etc/uhttpd.crt --tls-key /etc/uhttpd.key|' /etc/init.d/b4
            
            /etc/init.d/b4 enable >/dev/null 2>&1
        fi
    fi

    if [ "$QUIET_MODE" = "0" ]; then
        setup_geodat

        # Print installation summary
        echo ""
        print_info "Binary installed to: ${INSTALL_DIR}/${BINARY_NAME}"
        print_info "Configuration at: ${CONFIG_FILE}"
        echo ""
        print_info "To see all B4 options:"
        print_info "  ${INSTALL_DIR}/${BINARY_NAME} --help"

        # Check PATH
        if ! echo "$PATH" | grep -q "$INSTALL_DIR"; then
            print_warning "Note: $INSTALL_DIR is not in your PATH"
            print_info "You may want to add it to your PATH or create a symlink:"
            print_info "  ln -s ${INSTALL_DIR}/${BINARY_NAME} /usr/bin/${BINARY_NAME}"
        fi

        print_web_interface_info

        echo ""
        print_success "Installation finished successfully!"
        echo ""
        printf "${CYAN}Start B4 service now? (Y/n): ${NC}"
        read answer </dev/tty || answer="y"

        if [ -z "$answer" ]; then
            answer="y"
        fi

        case "$answer" in
        [nN] | [nN][oO])
            print_info "Service not started. Start manually when ready."
            ;;
        *)
            if [ -f "/etc/systemd/system/b4.service" ] && command_exists systemctl; then
                systemctl restart b4 2>/dev/null && print_success "Service started"
            elif [ -f "/opt/etc/init.d/S99b4" ]; then
                /opt/etc/init.d/S99b4 restart 2>/dev/null && print_success "Service started"
            elif [ -f "/etc/init.d/b4" ]; then
                /etc/init.d/b4 restart 2>/dev/null && print_success "Service started"
            fi
            ;;
        esac
        echo ""

        echo "======================================="
        echo "       Installation Complete!"
        echo "======================================="
    fi

}
