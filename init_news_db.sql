CREATE TABLE IF NOT EXISTS news (
    id SERIAL PRIMARY KEY,
    title VARCHAR(500) NOT NULL,
    content TEXT,
    description TEXT,
    link VARCHAR(1000) UNIQUE,
    pub_date TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_news_pub_date ON news(pub_date DESC);
CREATE INDEX IF NOT EXISTS idx_news_title ON news USING gin(to_tsvector('russian', title));
CREATE INDEX IF NOT EXISTS idx_news_content ON news USING gin(to_tsvector('russian', content));