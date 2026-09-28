# Privacy statement: registration section (draft for approval)

Status: **draft, 27 Sep 2026**, for Gernot to approve. Goes into
`_pages/imprint-privacy.md` in the go-live PR (Plan 3, Task 1, Step 4).

The existing statement (dated 1 January 2023) is German only and
generator-based (the `m…` ids). The German text below follows its structure:
an `<h2>`, one explanatory paragraph, and a `ul.m-elements` list. Insert it
after "Kontakt- und Anfragenverwaltung" (`#m182`), add it to the table of
contents, and update the "Stand:" date. The English text is for an English
version of the page, if you want one; the page currently has none.

## Before you publish: three things only you can decide or check

1. **Data processing agreements.** Mailjet: the DPA is part of their terms;
   check that it covers the `arc42-registration` sub-account. Fly.io: their
   DPA is in the dashboard or on their legal pages. Both texts below assume
   the DPAs are in place.
2. **Postal addresses.** The provider addresses are placeholders on purpose:
   they could not be confirmed from the providers' web pages on 27 Sep 2026.
   Take them from the signed DPAs, which name the contracting entity. Fly.io,
   Inc. is a US company (the machine runs in Amsterdam); its privacy policy
   states that it complies with the EU-US Data Privacy Framework (checked
   27 Sep 2026), so the text names the DPF as the transfer basis.
3. **Formspark is missing today.** Registrations currently go through
   Formspark (and Botpoison against spam), and the current statement names
   neither. Until Formspark is retired, the "Übergangszeit" paragraph below
   should be in the statement too; it can be published **now**, independent of
   the go-live.

Facts the text relies on (checked in `registration-app/` on 27 Sep 2026):
- The service stores nothing after a request: no database, no files.
- Its logs contain the registration id, the booking code, the language and
  the reason a submission was dropped. No names, e-mail addresses or IP
  addresses.
- The IP address is kept in memory for at most one hour, for the rate limit
  (5 registrations per address per hour; IPv6 per /64).
- The confirm link carries the registration data encrypted (AES-GCM, key
  known only to the service) and expires after 5 days.
- Mailjet open and click tracking is off, per message and in the account.
- No cookies, no analytics in the service.

---

## Deutsch

```html
<h2 id="m-anmeldung">Anmeldung zu Schulungen</h2>
<p>Wenn Sie sich über das Formular auf trainings.arc42.org zu einer Schulung
anmelden, verarbeiten wir die Angaben im Formular, um Ihre Anmeldung zu
bearbeiten, den Platz zu reservieren und die Rechnung zu stellen. Das
Formular wird von einem eigenen Anmeldedienst entgegengenommen, der beim
Anbieter Fly.io in einem Rechenzentrum in Amsterdam läuft. Er speichert die
Angaben nicht, sondern gibt sie per E-Mail an unser Schulungsbüro weiter und
schickt Ihnen eine E-Mail mit einem Bestätigungslink. Erst wenn Sie diesen
Link öffnen und die Anmeldung bestätigen, gilt sie als bestätigt. Der Link
enthält Ihre Angaben verschlüsselt und ist fünf Tage gültig. Die E-Mails
versenden wir über den Dienst Mailjet; Öffnungs- und Klickverfolgung sind
dabei abgeschaltet.</p>
<p>Zum Schutz vor automatisierten Anmeldungen verarbeitet der Dienst Ihre
IP-Adresse für höchstens eine Stunde im Arbeitsspeicher, um die Zahl der
Anmeldungen je Adresse zu begrenzen. In den Protokollen des Dienstes stehen
nur die Anmeldenummer, der Buchungscode und gegebenenfalls der Grund, aus dem
eine Anmeldung verworfen wurde, aber weder Namen noch E-Mail- oder
IP-Adressen. Die E-Mails mit Ihrer Anmeldung bewahren wir auf, solange es für
die Durchführung der Schulung und die gesetzlichen Aufbewahrungspflichten
(insbesondere für Rechnungen) erforderlich ist.</p>
<ul class="m-elements">
<li><strong>Verarbeitete Datenarten:</strong> Bestandsdaten (z.B. Namen,
Rechnungsadresse); Kontaktdaten (E-Mail-Adressen der anmeldenden und der
teilnehmenden Person); Vertragsdaten (gebuchte Schulung, Termin, Buchungscode);
Inhaltsdaten (Bemerkungen); Meta-/Kommunikationsdaten (IP-Adresse, Zeitpunkt
der Anmeldung).</li>
<li><strong>Betroffene Personen:</strong> Interessenten und Kunden;
teilnehmende Personen, die von einer anderen Person angemeldet werden.</li>
<li><strong>Zwecke der Verarbeitung:</strong> Bearbeitung von Anmeldungen und
Durchführung von Schulungen; Rechnungsstellung; Kommunikation;
Sicherheitsmaßnahmen (Schutz vor automatisierten Anmeldungen).</li>
<li><strong>Rechtsgrundlagen:</strong> Vertragserfüllung und vorvertragliche
Anfragen (Art. 6 Abs. 1 S. 1 lit. b DSGVO); rechtliche Verpflichtung
(Art. 6 Abs. 1 S. 1 lit. c DSGVO) für die Aufbewahrung von Rechnungen;
berechtigte Interessen (Art. 6 Abs. 1 S. 1 lit. f DSGVO) für den Schutz vor
Missbrauch.</li>
</ul>
<p><strong>Eingesetzte Dienste und Diensteanbieter:</strong></p>
<ul class="m-elements">
<li><strong>Fly.io:</strong> Betrieb des Anmeldedienstes (Server-Standort
Amsterdam); <strong>Dienstanbieter:</strong> Fly.io, Inc., USA
<em>[Anschrift aus dem AVV übernehmen]</em>; <strong>Website:</strong>
<a href="https://fly.io" target="_blank">https://fly.io</a>;
<strong>Datenschutzerklärung:</strong>
<a href="https://fly.io/legal/privacy-policy/" target="_blank">https://fly.io/legal/privacy-policy/</a>;
<strong>Auftragsverarbeitungsvertrag:</strong> abgeschlossen.
<strong>Grundlage Drittlandübermittlung:</strong> EU-US Data Privacy
Framework (DPF).</li>
<li><strong>Mailjet:</strong> Versand der E-Mails zur Anmeldung;
<strong>Dienstanbieter:</strong> Mailjet SAS, Paris, Frankreich
<em>[Anschrift aus dem AVV übernehmen]</em>; <strong>Website:</strong>
<a href="https://www.mailjet.com" target="_blank">https://www.mailjet.com</a>;
<strong>Datenschutzerklärung:</strong>
<a href="https://www.mailjet.com/legal/privacy-policy/" target="_blank">https://www.mailjet.com/legal/privacy-policy/</a>;
<strong>Auftragsverarbeitungsvertrag:</strong> abgeschlossen.</li>
<li><strong>Brevo</strong> <em>[nur falls statt Mailjet eingesetzt; den
nicht genutzten Dienst streichen]</em>: Versand der E-Mails zur Anmeldung;
<strong>Dienstanbieter:</strong> Brevo (Sendinblue)
<em>[Vertragspartner (Sendinblue France SAS oder Sendinblue Germany GmbH) und Anschrift aus dem AVV übernehmen]</em>; <strong>Website:</strong>
<a href="https://www.brevo.com" target="_blank">https://www.brevo.com</a>;
<strong>Datenschutzerklärung:</strong>
<a href="https://www.brevo.com/legal/privacypolicy/" target="_blank">https://www.brevo.com/legal/privacypolicy/</a>;
<strong>Auftragsverarbeitungsvertrag:</strong> abgeschlossen.</li>
</ul>
```

### Übergangszeit (solange Formspark noch Anmeldungen annimmt)

Gilt heute schon und bis Formspark abgeschaltet ist. Danach entfernen.

```html
<p><strong>Übergangszeit:</strong> Anmeldungen über ältere Formularseiten
werden noch vom Dienst Formspark entgegengenommen, der sie per E-Mail an uns
weiterleitet. Zum Schutz vor automatisierten Anmeldungen setzen diese Seiten
den Dienst Botpoison ein. <em>[Anbieter, Adresse, Datenschutzerklärung und
Drittlandgrundlage beider Dienste ergänzen; beide werden nach der Umstellung
abgeschaltet.]</em></p>
```

---

## English

```html
<h2 id="m-registration">Registration for trainings</h2>
<p>When you register for a training using the form on trainings.arc42.org, we
process the details you enter to handle your registration, reserve your place
and invoice you. The form is received by our own registration service, which
runs with the provider Fly.io in a data centre in Amsterdam. The service does
not store your details. It forwards them by e-mail to our training office and
sends you an e-mail with a confirmation link. Your registration counts as
confirmed only once you open that link and confirm. The link contains your
details in encrypted form and is valid for five days. We send these e-mails
through the Mailjet service, with open and click tracking switched off.</p>
<p>To protect against automated registrations, the service keeps your IP
address in memory for at most one hour, to limit the number of registrations
per address. The service's logs contain only the registration number, the
booking code and, where applicable, the reason a submission was discarded;
they contain no names, e-mail addresses or IP addresses. We keep the e-mails
about your registration for as long as we need them to run the training and
to meet statutory retention obligations (in particular for invoices).</p>
<ul class="m-elements">
<li><strong>Types of data processed:</strong> master data (e.g. names, billing
address); contact data (e-mail addresses of the registering and the
participating person); contract data (booked training, date, booking code);
content data (comments); meta and communication data (IP address, time of
registration).</li>
<li><strong>Data subjects:</strong> prospective and existing customers;
participants registered by another person.</li>
<li><strong>Purposes of processing:</strong> handling registrations and
running trainings; invoicing; communication; security measures (protection
against automated registrations).</li>
<li><strong>Legal bases:</strong> performance of a contract and pre-contractual
requests (Art. 6(1)(b) GDPR); legal obligation (Art. 6(1)(c) GDPR) for
retaining invoices; legitimate interests (Art. 6(1)(f) GDPR) for protection
against abuse.</li>
</ul>
<p><strong>Services and service providers used:</strong></p>
<ul class="m-elements">
<li><strong>Fly.io:</strong> hosting of the registration service (server
location Amsterdam); <strong>provider:</strong> Fly.io, Inc., USA
<em>[address as in the DPA]</em>; <strong>website:</strong>
<a href="https://fly.io" target="_blank">https://fly.io</a>;
<strong>privacy policy:</strong>
<a href="https://fly.io/legal/privacy-policy/" target="_blank">https://fly.io/legal/privacy-policy/</a>;
<strong>data processing agreement:</strong> in place.
<strong>Basis for third-country transfer:</strong> EU-US Data Privacy
Framework (DPF).</li>
<li><strong>Mailjet:</strong> sending the registration e-mails;
<strong>provider:</strong> Mailjet SAS, Paris, France
<em>[address as in the DPA]</em>; <strong>website:</strong>
<a href="https://www.mailjet.com" target="_blank">https://www.mailjet.com</a>;
<strong>privacy policy:</strong>
<a href="https://www.mailjet.com/legal/privacy-policy/" target="_blank">https://www.mailjet.com/legal/privacy-policy/</a>;
<strong>data processing agreement:</strong> in place.</li>
<li><strong>Brevo</strong> <em>[only if used instead of Mailjet; delete the
unused service]</em>: sending the registration e-mails;
<strong>provider:</strong> Brevo (Sendinblue)
<em>[contracting entity (Sendinblue France SAS or Sendinblue Germany GmbH) and address as in the DPA]</em>; <strong>website:</strong>
<a href="https://www.brevo.com" target="_blank">https://www.brevo.com</a>;
<strong>privacy policy:</strong>
<a href="https://www.brevo.com/legal/privacypolicy/" target="_blank">https://www.brevo.com/legal/privacypolicy/</a>;
<strong>data processing agreement:</strong> in place.</li>
</ul>
```

### Transition period (while Formspark still accepts registrations)

```html
<p><strong>Transition period:</strong> registrations made on older form pages
are still received by the Formspark service, which forwards them to us by
e-mail. To protect against automated registrations, these pages use the
Botpoison service. <em>[add provider, address, privacy policy and
third-country basis for both; both are switched off after the changeover.]</em></p>
```
