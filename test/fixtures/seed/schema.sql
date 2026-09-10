CREATE TABLE IF NOT EXISTS customers (
    id INT PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    email VARCHAR(100),
    created_at VARCHAR(30) NOT NULL
);

CREATE TABLE IF NOT EXISTS accounts (
    id INT PRIMARY KEY,
    customer_id INT NOT NULL,
    account_number VARCHAR(30) NOT NULL,
    balance DECIMAL(18,2) NOT NULL,
    status VARCHAR(20) NOT NULL
);

CREATE TABLE IF NOT EXISTS invoices (
    id INT PRIMARY KEY,
    customer_id INT NOT NULL,
    invoice_number VARCHAR(50) NOT NULL,
    amount DECIMAL(18,2) NOT NULL,
    issued_date VARCHAR(20) NOT NULL,
    paid INT NOT NULL
);

CREATE TABLE IF NOT EXISTS transactions (
    id INT PRIMARY KEY,
    account_id INT NOT NULL,
    amount DECIMAL(18,2) NOT NULL,
    transaction_type VARCHAR(20) NOT NULL,
    description VARCHAR(200),
    transaction_date VARCHAR(30) NOT NULL
);
