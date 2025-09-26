# PHP-FPM Docker Image with Optimized Configuration
# Supports multiple pool configurations

FROM php:8.2-fpm-alpine

# Build arguments
ARG POOL_NAME=web

# Install system dependencies
RUN apk add --no-cache \
    curl \
    git \
    zip \
    unzip \
    sqlite \
    sqlite-dev \
    libpng-dev \
    libjpeg-turbo-dev \
    libwebp-dev \
    freetype-dev \
    libxml2-dev \
    libxslt-dev \
    icu-dev \
    libzip-dev \
    oniguruma-dev \
    autoconf \
    g++ \
    make

# Install PHP extensions
RUN docker-php-ext-configure gd \
        --with-freetype \
        --with-jpeg \
        --with-webp \
    && docker-php-ext-install -j$(nproc) \
        bcmath \
        exif \
        gd \
        intl \
        mbstring \
        opcache \
        pdo \
        pdo_sqlite \
        soap \
        xsl \
        zip

# Install Redis extension
RUN pecl install redis && docker-php-ext-enable redis

# Install APCu for user cache
RUN pecl install apcu && docker-php-ext-enable apcu

# Install Composer
COPY --from=composer:2.6 /usr/bin/composer /usr/bin/composer

# Create necessary directories
RUN mkdir -p /var/www/html /var/log/php-fpm /var/lib/php/sessions && \
    chown -R www-data:www-data /var/www/html /var/log/php-fpm /var/lib/php/sessions

# Copy PHP configuration
COPY php.ini /usr/local/etc/php/php.ini

# Copy PHP-FPM pool configuration template
COPY pools/ /usr/local/etc/php-fpm.d/

# Create healthcheck script
RUN echo '#!/bin/sh' > /usr/local/bin/healthcheck && \
    echo 'SCRIPT_NAME=/ping SCRIPT_FILENAME=/ping REQUEST_METHOD=GET cgi-fcgi -bind -connect 127.0.0.1:9000' >> /usr/local/bin/healthcheck && \
    chmod +x /usr/local/bin/healthcheck

# Install fcgi for healthcheck
RUN apk add --no-cache fcgi

# Switch to www-data user
USER www-data

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD /usr/local/bin/healthcheck

# Expose PHP-FPM port
EXPOSE 9000

# Start PHP-FPM
CMD ["php-fpm", "-F"]