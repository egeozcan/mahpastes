// Bounded CSV/TSV table Preview.
//
// The same security property the source renderer holds, for the same reason: cell
// values become text nodes and nothing else. No source text ever reaches
// innerHTML, no value becomes an attribute the document chose, and no URL in a
// cell becomes a link. A CSV field containing `<script>` is a cell that reads
// `<script>`.
//
// Rendering is bounded twice over. The parse is capped (see delimited.js), and
// presentation stops far earlier — 500 rows, 100 columns, 10,000 cells, whichever
// comes first — with a visible notice pointing at Edit for the complete source. A
// preview that tried to lay out a 100,000-row table would not be a preview.
//
// Above the enhanced-assistance threshold this renderer is never called at all:
// TextPreview forces plain inert source for every format there, table included.

import { DIAGNOSTIC_LIMITS } from './diagnostics.js';
import {
    analyzeDelimited,
    delimiterLabelFor,
    delimiterLabelForID,
    DELIMITERS,
    TABLE_LIMITS,
} from './delimited.js';

function number(value) {
    return value.toLocaleString('en-US');
}

function note(text) {
    const paragraph = document.createElement('p');
    paragraph.className = 'table-preview-note';
    paragraph.textContent = text;
    return paragraph;
}

function plainSource(source, wrap) {
    const pre = document.createElement('pre');
    pre.className = 'source-preview-plain';
    pre.dataset.wrap = wrap ? 'on' : 'off';
    pre.appendChild(document.createTextNode(source));
    return pre;
}

function cell(tag, text, className) {
    const node = document.createElement(tag);
    if (className) node.className = className;
    node.appendChild(document.createTextNode(text));
    return node;
}

/**
 * Builds the `<table>`.
 *
 * A real table with real header semantics, not a grid of divs: `<th scope="col">`
 * for the column strip and `<th scope="row">` for the row-number gutter, so a
 * screen reader announces a cell's column and row rather than reading a wall of
 * values.
 *
 * The row-number gutter is deliberately outside the column and cell budgets. It
 * is a gutter, the table equivalent of the source preview's line numbers, and
 * charging it against "100 columns" would silently cost the user a column of data.
 */
function buildTable(records, { header, rows, columns, headerRecord }) {
    const table = document.createElement('table');
    table.className = 'table-preview-table';

    const head = document.createElement('thead');
    const headRow = document.createElement('tr');
    headRow.appendChild(cell('th', '', 'table-preview-row-number'));
    for (let c = 0; c < columns; c++) {
        // With no header row the strip carries column numbers, so the table still
        // has header semantics and the user still has something to count against.
        const label = header ? String(headerRecord[c] === undefined ? '' : headerRecord[c]) : String(c + 1);
        const th = cell('th', label, header ? null : 'table-preview-column-number');
        th.setAttribute('scope', 'col');
        if (header && headerRecord[c] === undefined) th.dataset.padded = 'true';
        headRow.appendChild(th);
    }
    head.appendChild(headRow);
    table.appendChild(head);

    const body = document.createElement('tbody');
    for (let r = 0; r < rows; r++) {
        const record = records[r];
        const tr = document.createElement('tr');
        const rowNumber = cell('th', String(r + 1), 'table-preview-row-number');
        rowNumber.setAttribute('scope', 'row');
        tr.appendChild(rowNumber);
        for (let c = 0; c < columns; c++) {
            const value = record[c];
            const td = cell('td', value === undefined ? '' : value);
            // Missing display cells are padded so the grid stays rectangular. The
            // source is untouched — ragged records are frequently intentional, and
            // this marker is what lets a test prove the padding is presentational.
            if (value === undefined) td.dataset.padded = 'true';
            tr.appendChild(td);
        }
        body.appendChild(tr);
    }
    table.appendChild(body);
    return table;
}

export function createTablePreviewRenderer() {
    /**
     * Renders the current unsaved source as a table.
     *
     * `delimiterID` and `headerMode` are the user's temporary interpretation
     * choices, passed in per render rather than held here — this renderer keeps no
     * per-clip state, which is what makes "does not persist after close" true by
     * construction rather than by remembering to reset something.
     */
    function render({
        source = '',
        preferredID = null,
        delimiterID = null,
        headerMode = 'auto',
        wrap = true,
        generation = 0,
    } = {}) {
        const root = document.createElement('div');
        root.className = 'table-preview';
        root.dataset.wrap = wrap ? 'on' : 'off';
        root.dataset.generation = String(generation);

        let analysis;
        try {
            analysis = analyzeDelimited(source, { preferredID, delimiterID, headerMode });
        } catch (err) {
            // A renderer failure falls back to inert source plus a possible issue,
            // never to an error screen: the content is still readable and copyable,
            // and hiding it would be a worse answer than showing it unparsed.
            root.dataset.mode = 'failed';
            root.appendChild(plainSource(source, wrap));
            return {
                node: root,
                mode: 'failed',
                diagnostics: [{
                    severity: 'possible-issue',
                    source: 'table-preview',
                    // Fixed text for the same reason the other renderers use it: an
                    // exception message can contain the document.
                    message: 'Table preview is unavailable for this document. Showing source instead.',
                    line: 1,
                    column: 1,
                }],
                generation,
                interpretation: null,
            };
        }

        const records = analysis.records;
        const headerRecord = analysis.header ? records[0] : null;
        const dataRecords = analysis.header ? records.slice(1) : records;
        const widest = records.reduce((max, record) => Math.max(max, record.length), 0);

        const columns = Math.min(widest, TABLE_LIMITS.renderColumns);
        // Total cells is a joint bound on rows and columns, so once the column count
        // is known it converts into a row ceiling. With 100 columns that is 100
        // rows, not 500 — "stopping at the first reached" in practice.
        // The header row is rendered and therefore spends from the same budget. Not
        // charging it let a 100-column header plus 100 data rows render 10,100 cells.
        const headerRows = headerRecord ? 1 : 0;
        const cellRowCap = columns > 0
            ? Math.max(0, Math.floor(TABLE_LIMITS.renderCells / columns) - headerRows)
            : TABLE_LIMITS.renderRows;
        const rows = Math.max(0, Math.min(dataRecords.length, TABLE_LIMITS.renderRows, cellRowCap));

        const columnsTruncated = widest > columns;
        const rowsTruncated = dataRecords.length > rows;
        const cellsBound = rowsTruncated && cellRowCap < Math.min(dataRecords.length, TABLE_LIMITS.renderRows);

        const notes = [];
        if (analysis.inconclusive) {
            notes.push(
                'No delimiter could be determined from this document, so comma is being used. ' +
                'Use the Delimiter control above to choose one.',
            );
        }
        if (analysis.limit) {
            const bound = analysis.limit === 'fields'
                ? `${number(DIAGNOSTIC_LIMITS.csvFields)} fields`
                : `${number(DIAGNOSTIC_LIMITS.csvRecords)} records`;
            notes.push(
                `Parsing stopped at the safety limit of ${bound}, so this table covers only the ` +
                'beginning of the document. Switch to Edit for the complete source.',
            );
        }
        if (rowsTruncated || columnsTruncated) {
            const parts = [];
            if (rowsTruncated) parts.push(`the first ${number(rows)} of ${number(dataRecords.length)} rows`);
            if (columnsTruncated) parts.push(`the first ${number(columns)} of ${number(widest)} columns`);
            let text = `This table is truncated: showing ${parts.join(' and ')}.`;
            if (cellsBound) text += ` The ${number(TABLE_LIMITS.renderCells)}-cell limit was reached first.`;
            notes.push(`${text} Switch to Edit for the complete source.`);
        }
        for (const text of notes) root.appendChild(note(text));

        if (columns === 0) {
            root.dataset.mode = 'empty';
            root.appendChild(note('This document has no delimited records to show.'));
        } else {
            root.dataset.mode = 'table';
            root.dataset.delimiter = analysis.delimiterID;
            root.dataset.header = analysis.header ? 'true' : 'false';
            const scroll = document.createElement('div');
            // Wide tables scroll inside their own container rather than making the
            // whole panel scroll sideways.
            scroll.className = 'table-preview-scroll';
            scroll.appendChild(buildTable(dataRecords, { header: analysis.header, rows, columns, headerRecord }));
            root.appendChild(scroll);
        }

        return {
            node: root,
            mode: root.dataset.mode,
            // Findings about the *content* — malformed quoting, ragged widths — come
            // from the validator, on the debounced generation-guarded path, so they
            // stay correct in Edit mode too. This renderer contributes a diagnostic
            // only when it fails outright.
            diagnostics: [],
            generation,
            // Handed back so the control bar can label what was detected without
            // reparsing, and so tests can assert the interpretation directly.
            interpretation: {
                delimiter: analysis.delimiter,
                delimiterID: analysis.delimiterID,
                delimiterLabel: delimiterLabelFor(analysis.delimiter),
                delimiterExplicit: analysis.delimiterExplicit,
                // What detection chose, whether or not it is what is in force. The
                // control labels its "Detected" option from this, so an override
                // never makes the auto option look like agreement.
                detectedDelimiterID: analysis.detectedDelimiterID,
                detectedDelimiterLabel: delimiterLabelForID(analysis.detectedDelimiterID),
                detectionInconclusive: analysis.detectionInconclusive,
                inconclusive: analysis.inconclusive,
                header: analysis.header,
                headerDetected: analysis.headerDetected,
                headerMode: analysis.headerMode,
                records: records.length,
                dataRecords: dataRecords.length,
                widestRecord: widest,
                modalWidth: analysis.modalWidth,
                deviants: analysis.deviants,
                renderedRows: rows,
                renderedColumns: columns,
                rowsTruncated,
                columnsTruncated,
                cellsBound,
                parseLimit: analysis.limit,
            },
        };
    }

    return { render };
}

export const TablePreviewRenderer = {
    create: createTablePreviewRenderer,
    LIMITS: TABLE_LIMITS,
    // The delimiter ids the interpretation control's option values must use. An
    // unrecognized id silently falls back to detection, so the option list and this
    // one are pinned against each other by test rather than trusted to stay in step.
    DELIMITERS: DELIMITERS.map(({ id, label }) => ({ id, label })),
};
