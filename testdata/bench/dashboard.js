const palette = {
  ink: '#17212b',
  rust: '#bb5a3c',
  foam: '#d8eef2',
  gold: '#d9aa5a'
};

function makeSeries(seed, size) {
  const values = [];
  let state = seed;
  for (let index = 0; index < size; index += 1) {
    state = (state * 1103515245 + 12345) & 0x7fffffff;
    values.push({
      index,
      value: 40 + (state % 61),
      movingAverage: 44 + ((state >> 3) % 37),
      anomaly: (state & 31) === 0
    });
  }
  return values;
}

function makeCard(title, series) {
  return {
    title,
    last: series[series.length - 1].value,
    peak: Math.max(...series.map(point => point.value)),
    trough: Math.min(...series.map(point => point.value)),
    average: Math.round(series.reduce((sum, point) => sum + point.value, 0) / series.length),
    anomalies: series.filter(point => point.anomaly).length,
    color: palette.rust,
    gridColor: 'rgba(23, 33, 43, 0.12)',
    labelColor: palette.ink,
    values: series
  };
}

export function buildDashboardPayload() {
  const traffic = makeSeries(77, 96);
  const pressure = makeSeries(81, 96);
  const temperature = makeSeries(99, 96);
  return {
    generatedAt: '2026-03-30T06:00:00Z',
    locale: 'en-US',
    theme: palette,
    cards: [
      makeCard('Traffic', traffic),
      makeCard('Pressure', pressure),
      makeCard('Temperature', temperature)
    ],
    notifications: [
      { level: 'info', message: 'Night shift completed inspection list.' },
      { level: 'warning', message: 'Boiler room humidity above normal range.' },
      { level: 'info', message: 'Archive rotation scheduled for Tuesday.' }
    ]
  };
}

export function renderRows(payload) {
  return payload.cards.map(card => {
    const anomalyRate = `${card.anomalies}/${card.values.length}`;
    return `${card.title.padEnd(14)} ${String(card.last).padStart(3)} ${String(card.average).padStart(3)} ${anomalyRate}`;
  }).join('\n');
}

if (typeof window !== 'undefined') {
  const payload = buildDashboardPayload();
  window.__dashboardPayload = payload;
  window.__dashboardRows = renderRows(payload);
}