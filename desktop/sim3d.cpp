#include "sim3d.h"

#include <QDateTime>
#include <QFile>
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QNetworkReply>
#include <QNetworkRequest>
#include <QUrlQuery>
#include <cmath>

namespace {

// The width of a strand: the line of a 0.5 mm nozzle.
constexpr float lineWidth = 0.55f;
// The vertex: a position and a normal, 6 floats.
constexpr int vertexStride = 6 * sizeof(float);

void put(QByteArray &b, QVector3D p, QVector3D n)
{
    const float v[6] = {p.x(), p.y(), p.z(), n.x(), n.y(), n.z()};
    b.append(reinterpret_cast<const char *>(v), sizeof(v));
}

// quad adds two triangles: a b c, a c d, with the normal n.
void quad(QByteArray &b, QVector3D a, QVector3D bb, QVector3D c, QVector3D d, QVector3D n)
{
    put(b, a, n);
    put(b, bb, n);
    put(b, c, n);
    put(b, a, n);
    put(b, c, n);
    put(b, d, n);
}

// scene turns a point of the printer (x right, y back, z up) into the scene (x right, y up, z to
// the viewer).
QVector3D scene(float x, float y, float z) { return QVector3D(x, z, -y); }

} // namespace

PathGeometry::PathGeometry(QQuick3DObject *parent) : QQuick3DGeometry(parent)
{
    setPrimitiveType(PrimitiveType::Triangles);
    setStride(vertexStride);
    addAttribute(Attribute::PositionSemantic, 0, Attribute::F32Type);
    addAttribute(Attribute::NormalSemantic, 3 * sizeof(float), Attribute::F32Type);
}

void PathGeometry::add(const QList<float> &segs)
{
    for (int i = 0; i + 5 < segs.size(); i += 6) {
        float x0 = segs[i], y0 = segs[i + 1], z0 = segs[i + 2], x1 = segs[i + 3], y1 = segs[i + 4], z1 = segs[i + 5];
        // The height of a strand is the step from the layer under it.
        if (z1 > m_lastZ + 0.01f) {
            if (m_lastZ > 0 && z1 - m_lastZ < 1.0f)
                m_layer = std::clamp(z1 - m_lastZ, 0.08f, 0.6f);
            m_lastZ = z1;
        }
        float dx = x1 - x0, dy = y1 - y0, len = std::hypot(dx, dy);
        if (len < 0.01f)
            continue;
        // The side of the strand: half the width, across the move.
        float sx = -dy / len * lineWidth / 2, sy = dx / len * lineWidth / 2;
        float top = z1, bot = z1 - m_layer;
        QVector3D a = scene(x0 + sx, y0 + sy, top), b = scene(x1 + sx, y1 + sy, top);
        QVector3D c = scene(x1 - sx, y1 - sy, top), d = scene(x0 - sx, y0 - sy, top);
        QVector3D ab = scene(x0 + sx, y0 + sy, bot), bb = scene(x1 + sx, y1 + sy, bot);
        QVector3D cb = scene(x1 - sx, y1 - sy, bot), db = scene(x0 - sx, y0 - sy, bot);
        QVector3D side = scene(sx, sy, 0).normalized();
        quad(m_vertices, a, b, c, d, QVector3D(0, 1, 0)); // the top
        quad(m_vertices, ab, bb, b, a, side);              // one side
        quad(m_vertices, c, cb, db, d, -side);             // the other side
        for (QVector3D p : {a, c, bb, db}) {
            if (m_moves == 0 && p == a)
                m_min = m_max = p;
            m_min = QVector3D(std::min(m_min.x(), p.x()), std::min(m_min.y(), p.y()), std::min(m_min.z(), p.z()));
            m_max = QVector3D(std::max(m_max.x(), p.x()), std::max(m_max.y(), p.y()), std::max(m_max.z(), p.z()));
        }
        m_moves++;
    }
    upload();
}

void PathGeometry::set(const QList<float> &segs, float layer)
{
    m_vertices.clear();
    m_moves = 0;
    m_lastZ = 0;
    m_layer = layer;
    add(segs);
}

void PathGeometry::clear()
{
    m_vertices.clear();
    m_moves = 0;
    m_lastZ = 0;
    m_layer = 0.3f;
    upload();
}

void PathGeometry::upload()
{
    setVertexData(m_vertices);
    setBounds(m_min, m_max);
    update();
    emit movesChanged();
}

MeshGeometry::MeshGeometry(QQuick3DObject *parent) : QQuick3DGeometry(parent)
{
    setPrimitiveType(PrimitiveType::Triangles);
    setStride(vertexStride);
    addAttribute(Attribute::PositionSemantic, 0, Attribute::F32Type);
    addAttribute(Attribute::NormalSemantic, 3 * sizeof(float), Attribute::F32Type);
}

void MeshGeometry::setSource(const QString &source)
{
    if (source == m_source)
        return;
    m_source = source;
    QFile f(source);
    QByteArray in;
    if (f.open(QIODevice::ReadOnly))
        in = f.readAll();
    else
        qWarning("MeshGeometry: %s: %s", qPrintable(source), qPrintable(f.errorString()));
    constexpr int fileStride = 10;
    const qsizetype n = in.size() / fileStride;
    QByteArray data;
    data.reserve(n * vertexStride);
    // The vertices as float32, and the bounds for the culling of the view.
    QVector3D lo, hi;
    for (qsizetype i = 0; i < n; i++) {
        const uchar *r = reinterpret_cast<const uchar *>(in.constData()) + i * fileStride;
        auto i16 = [r](int k) { return float(qint16(r[2 * k] | r[2 * k + 1] << 8)) / 50; };
        auto i8 = [r](int k) { return float(qint8(r[6 + k])) / 127; };
        QVector3D p(i16(0), i16(1), i16(2));
        put(data, p, QVector3D(i8(0), i8(1), i8(2)));
        if (i == 0)
            lo = hi = p;
        lo = QVector3D(std::min(lo.x(), p.x()), std::min(lo.y(), p.y()), std::min(lo.z(), p.z()));
        hi = QVector3D(std::max(hi.x(), p.x()), std::max(hi.y(), p.y()), std::max(hi.z(), p.z()));
    }
    setVertexData(data);
    setBounds(lo, hi);
    update();
    emit sourceChanged();
}

void ImageTexture::setImage(const QImage &image)
{
    m_image = image;
    if (!image.isNull()) {
        QImage rgba = image.convertToFormat(QImage::Format_RGBA8888);
        setSize(rgba.size());
        setFormat(QQuick3DTextureData::RGBA8);
        setTextureData(QByteArray(reinterpret_cast<const char *>(rgba.constBits()), rgba.sizeInBytes()));
    }
    emit imageChanged();
}

SimFeed::SimFeed(QObject *parent) : QObject(parent)
{
    m_stateTimer.setInterval(33); // 30 times a second: the moves of the printer
    m_pathTimer.setInterval(1000);
    connect(&m_stateTimer, &QTimer::timeout, this, &SimFeed::pollState);
    connect(&m_pathTimer, &QTimer::timeout, this, &SimFeed::pollPath);
    m_lcdTimer.setInterval(250);
    connect(&m_lcdTimer, &QTimer::timeout, this, &SimFeed::pollLCD);
}

void SimFeed::start(const QUrl &base, const QByteArray &token)
{
    m_base = base;
    m_token = token;
    m_stateTimer.start();
    m_pathTimer.start();
    m_lcdTimer.start();
}

void SimFeed::setPath(PathGeometry *p)
{
    if (m_path == p)
        return;
    m_path = p;
    m_key.clear();
    m_next = 0;
    m_pending.clear();
    m_laid.clear();
    emit pathChanged();
}

void SimFeed::setRecent(PathGeometry *p)
{
    if (m_recent == p)
        return;
    m_recent = p;
    emit pathChanged();
}

// follow shows the moves that the nozzle has laid: the moves before the move that it is on now,
// and that move up to the nozzle. The file of the print is read ahead of the nozzle, so the moves
// wait in m_pending until the nozzle gets to them. At the end of a print, all of them show.
//
// An extruding move that is not in the file (the prime line and the other moves of the macros)
// leaves a trail: a strand from the last place of the nozzle to its place now.
void SimFeed::follow(bool busy)
{
    if (!m_path || !m_recent)
        return;
    auto commit = [this] {
        if (!m_laid.isEmpty())
            m_path->add(m_laid);
        m_laid.clear();
        m_committed = QDateTime::currentMSecsSinceEpoch();
    };
    const QVector3D last = m_last;
    const bool hasLast = m_hasLast;
    m_last = m_pos;
    m_hasLast = true;
    auto trail = [&] {
        const float d = (m_pos - last).length();
        if (!m_extruding || !hasLast || d < 0.05f || d > 20)
            return;
        m_laid << last.x() << last.y() << last.z() << m_pos.x() << m_pos.y() << m_pos.z();
        if (QDateTime::currentMSecsSinceEpoch() - m_committed > 500)
            commit();
        m_recent->set(m_laid, m_path->layer());
    };
    if (!busy) {
        if (!m_pending.isEmpty() || !m_laid.isEmpty()) {
            m_laid += m_pending;
            m_pending.clear();
            commit();
            m_recent->clear();
        }
        trail();
        return;
    }
    // The move that the nozzle is on: the nearest one at its height, among the next moves.
    const float px = m_pos.x(), py = m_pos.y(), pz = m_pos.z();
    int best = -1;
    float bestD = 1.5f, bestT = 0;
    const int n = std::min<int>(m_pending.size() / 6, 400);
    for (int j = 0; j < n; j++) {
        const float *s = m_pending.constData() + 6 * j;
        if (std::abs(s[5] - pz) > 0.15f)
            continue;
        float dx = s[3] - s[0], dy = s[4] - s[1], l2 = dx * dx + dy * dy;
        float t = l2 > 0 ? std::clamp(((px - s[0]) * dx + (py - s[1]) * dy) / l2, 0.f, 1.f) : 1.f;
        float d = std::hypot(s[0] + t * dx - px, s[1] + t * dy - py);
        if (d < bestD) {
            best = j;
            bestD = d;
            bestT = t;
        }
    }
    if (best < 0) {
        trail(); // a travel move, a wait, or a move of a macro
        return;
    }
    m_laid += m_pending.mid(0, 6 * best);
    m_pending.remove(0, 6 * best);
    if (QDateTime::currentMSecsSinceEpoch() - m_committed > 500 || m_laid.size() > 6 * 800)
        commit();
    // The strands since the last commit, and the move up to the nozzle.
    QList<float> recent = m_laid;
    const float *s = m_pending.constData();
    recent << s[0] << s[1] << s[2] << s[0] + bestT * (s[3] - s[0]) << s[1] + bestT * (s[4] - s[1]) << s[5];
    m_recent->set(recent, m_path->layer());
}

QNetworkReply *SimFeed::get(const QString &path)
{
    QUrl url = m_base.resolved(QUrl(path));
    QNetworkRequest req(url);
    req.setRawHeader("X-Api-Key", m_token);
    req.setTransferTimeout(5000);
    return m_net.get(req);
}

void SimFeed::pollState()
{
    if (m_stateBusy)
        return;
    m_stateBusy = true;
    QNetworkReply *r = get(QStringLiteral("/sim/state"));
    connect(r, &QNetworkReply::finished, this, [this, r] {
        r->deleteLater();
        m_stateBusy = false;
        if (r->error() != QNetworkReply::NoError) {
            if (m_connected) {
                m_connected = false;
                emit stateChanged();
            }
            return;
        }
        QJsonObject o = QJsonDocument::fromJson(r->readAll()).object();
        // The place of the nozzle in mm, as the moves of the part.
        QJsonArray p = o.value(QStringLiteral("pos")).toArray();
        if (p.size() >= 3)
            m_pos = QVector3D(p[0].toDouble(), p[1].toDouble(), p[2].toDouble());
        m_extruding = o.value(QStringLiteral("ev")).toDouble() > 0.01;
        m_tool = o.value(QStringLiteral("tool")).toDouble();
        m_toolT = o.value(QStringLiteral("toolT")).toDouble();
        m_bed = o.value(QStringLiteral("bed")).toDouble();
        m_bedT = o.value(QStringLiteral("bedT")).toDouble();
        m_fan = o.value(QStringLiteral("fan")).toDouble();
        m_state = o.value(QStringLiteral("state")).toString();
        m_file = o.value(QStringLiteral("file")).toString();
        m_connected = true;
        follow(o.value(QStringLiteral("busy")).toBool());
        emit stateChanged();
    });
}

void SimFeed::pollPath()
{
    if (m_pathBusy || !m_path)
        return;
    m_pathBusy = true;
    QUrlQuery q;
    q.addQueryItem(QStringLiteral("key"), m_key);
    q.addQueryItem(QStringLiteral("from"), QString::number(m_next));
    QNetworkReply *r = get(QStringLiteral("/sim/path?") + q.toString(QUrl::FullyEncoded));
    connect(r, &QNetworkReply::finished, this, [this, r] {
        r->deleteLater();
        m_pathBusy = false;
        if (r->error() != QNetworkReply::NoError || !m_path)
            return;
        QJsonObject o = QJsonDocument::fromJson(r->readAll()).object();
        QString key = o.value(QStringLiteral("key")).toString();
        int from = o.value(QStringLiteral("from")).toInt();
        if (key != m_key || from == 0) {
            // A new print, or no print: the part starts again.
            if (key != m_key || m_next != 0) {
                m_path->clear();
                if (m_recent)
                    m_recent->clear();
                m_pending.clear();
                m_laid.clear();
            }
            m_key = key;
            m_next = 0;
        }
        QJsonArray s = o.value(QStringLiteral("segs")).toArray();
        QList<float> segs;
        segs.reserve(s.size());
        for (const QJsonValue &v : s)
            segs.append(float(v.toDouble()));
        m_pending += segs; // they show when the nozzle gets to them (follow)
        m_next = from + segs.size() / 6;
        // More moves wait: read them now.
        if (segs.size() / 6 >= 20000)
            QTimer::singleShot(0, this, &SimFeed::pollPath);
    });
}

void SimFeed::pollLCD()
{
    if (m_lcdBusy)
        return;
    m_lcdBusy = true;
    QNetworkReply *r = get(QStringLiteral("/sim/lcd.png"));
    connect(r, &QNetworkReply::finished, this, [this, r] {
        r->deleteLater();
        m_lcdBusy = false;
        if (r->error() != QNetworkReply::NoError)
            return;
        QImage img;
        if (img.loadFromData(r->readAll(), "PNG") && img != m_lcd) {
            m_lcd = img;
            emit lcdChanged();
        }
    });
}

void SimFeed::setScreen(const QImage &image)
{
    m_screen = image;
    emit screenChanged();
}
