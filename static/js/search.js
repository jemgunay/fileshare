var maxAutoCompleteSuggestions = 5;

$(document).ready(function() {

    if (window.location.pathname === "/") {
        // init search/filter inputs
        $("#desc-search-input").val("").on("input", performSearch);

        performRequest(hostname + "/data?fetch=tags,people,file_types,dates", "GET", "", function (result) {
            var tokenfieldSets = [["tags", "#tags-search-input", false], ["people", "#people-search-input", false], ["file_types", "#type-search-input", true]];
            var parsedData = JSON.parse(result);

            initMetaDataFields(parsedData, tokenfieldSets);

            // date pickers
            $("#min-date-picker, #max-date-picker").datetimepicker({
                format: "DD/MM/YYYY"
            });

            if (parsedData["dates"] == null) {
                var currentEpoch = Math.floor((new Date).getTime() / 1000);
                parsedData["dates"] = [currentEpoch, currentEpoch];
            }
            $("#min-date-picker").data("DateTimePicker").date(new Date(parseInt(parsedData["dates"][0]) * 1000));
            $("#max-date-picker").data("DateTimePicker").date(new Date(parseInt(parsedData["dates"][1]) * 1000));
            $("#min-date-picker, #max-date-picker").on("dp.change", performSearch);
        });
    }

});

// Pull required data from server & initialise search/filter inputs.
function initMetaDataFields(parsedData, tokenfieldSets) {
    // iterate over tokenfield types (tags, people & file_types) and initialise. If 3rd array value is true, tokenfield value will be populated pre-with all retrieved data.
    for (var i = 0; i < tokenfieldSets.length; i++) {
        var metaType = tokenfieldSets[i][0];
        var tagIDs = tokenfieldSets[i][1];
        var populateValue = tokenfieldSets[i][2];

        // check if data eists
        if (parsedData[metaType] == null) {
            continue;
        }

        var commaSeparatedData = parsedData[metaType].join();
        // set default input value
        if (populateValue) {
            $(tagIDs).val(commaSeparatedData);
        }

        (function (tagIDs, parsedData, metaType) {
            $(tagIDs).tokenfield({
                autocomplete: {
                    source: function (request, response) {
                        var results = $.ui.autocomplete.filter(parsedData[metaType], request.term);

                        // remove already selected tokens from autocomplete results
                        var selectedTokens = $(tagIDs).tokenfield('getTokens', false);
                        var selectedTokenVals = [];
                        for (var i in selectedTokens) {
                            selectedTokenVals.push(selectedTokens[i]["label"]);
                        }
                        var unselectedResults = $(results).not(selectedTokenVals).get();

                        // limit autocomplete results
                        response(unselectedResults.slice(0, maxAutoCompleteSuggestions))
                    },
                    delay: 0
                },
                showAutocompleteOnFocus: true,
                createTokensOnBlur: true
            }).on("tokenfield:createdtoken tokenfield:editedtoken tokenfield:removedtoken", performSearch);
        }(tagIDs, parsedData, metaType));
    }
}

// Perform search/filter request.
function performSearch() {
    // collect & format parameters from inputs
    var dates = [$("#min-date-picker").data("DateTimePicker").date(), $("#max-date-picker").data("DateTimePicker").date()];
    if (dates[0]) { dates[0] = dates[0].unix() }
    if (dates[1]) { dates[1] = dates[1].unix() }
    var tokenfieldTags = [$("#tags-search-input").tokenfield('getTokensList', ",", false), $("#people-search-input").tokenfield('getTokensList', ",", false), $("#type-search-input").tokenfield('getTokensList', ",", false)];
    
    var request = "/search?desc=" + $("#desc-search-input").val() + "&min_date=" + dates[0] + "&max_date=" + dates[1] + "&tags=" + tokenfieldTags[0] + "&people=" + tokenfieldTags[1] + "&file_types=" + tokenfieldTags[2] + "&format=html";
    
    // console.log(request);
    
    // perform search request
    performRequest(hostname + request, "GET", "", function(html) {
        if (html.length === 2) {
            performRequest(hostname + "/static/no_match.html", "GET", "", function(htmlNoMatch) {
                $("#results-window").empty().append(htmlNoMatch)
            });
            return
        }
        $("#results-window").empty().append(html)
    });
}