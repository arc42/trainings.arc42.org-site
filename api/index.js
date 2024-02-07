const fs = require('fs').promises;
const path = require('path');

const allowCors = fn => async (req, res) => {
  res.setHeader('Access-Control-Allow-Credentials', true);
  res.setHeader('Access-Control-Allow-Origin', '*');
  res.setHeader('Access-Control-Allow-Methods', 'GET,OPTIONS,PATCH,DELETE,POST,PUT');
  res.setHeader(
    'Access-Control-Allow-Headers',
    'X-CSRF-Token, X-Requested-With, Accept, Accept-Version, Content-Length, Content-MD5, Content-Type, Date, X-Api-Version, hx-target, hx-current-url, hx-request, hx-trigger'
  );
  if (req.method === 'OPTIONS') {
    res.status(200).end();
    return;
  }
  return await fn(req, res);
};

const handler = async (req, res) => {
  try {
    const delay = (duration) => new Promise(resolve => setTimeout(resolve, duration));

    await delay(6000);

    const filePath = path.join(__dirname, '..', '_includes', '_subtle-ads.html');
    const htmlContent = await fs.readFile(filePath, 'utf8');
    res.setHeader('Content-Type', 'text/html');
    res.status(200).end(htmlContent);
  } catch (error) {
    res.status(500).end('Error loading the HTML file.');
  }
};

module.exports = allowCors(handler);
